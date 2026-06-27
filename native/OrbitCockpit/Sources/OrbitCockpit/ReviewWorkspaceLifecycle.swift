import Foundation

enum ReviewWorkspacePersistenceState: String, Codable, Equatable {
    case active
    case cleanupRequired
    case missing
}

struct ReviewWorkspaceRecord: Codable, Equatable, Identifiable {
    // swiftlint:disable:next identifier_name
    let id: UUID
    let repository: RepositoryIdentity
    let pullRequestNumber: Int
    let pullRequestURL: URL
    let sourceClonePath: URL
    let sourceCloneRemoteURL: String
    let headRepository: RepositoryIdentity
    let headCloneURL: URL
    let headBranch: String
    let headSHA: String
    let worktreePath: URL
    let createdAt: Date
    var updatedAt: Date
    var state: ReviewWorkspacePersistenceState
}

struct OrphanedReviewWorkspace: Equatable {
    let sourceClonePath: URL
    let worktreePath: URL
}

struct ReviewWorkspaceReconciliationResult: Equatable {
    let records: [ReviewWorkspaceRecord]
    let orphanedWorktrees: [OrphanedReviewWorkspace]
}

enum ReviewWorkspaceCleanupResult: Equatable {
    case removed
    case cleanupRequired(ReviewWorkspaceRecord)
}

enum ReviewWorkspaceLifecycleError: Error, Equatable, LocalizedError {
    case invalidHeadSHA
    case worktreePathInsideSourceClone
    case worktreePathOutsideManagedRoot
    case fetchedRefMismatch(expected: String, actual: String)
    case checkedOutHeadMismatch(expected: String, actual: String)

    var errorDescription: String? {
        switch self {
        case .invalidHeadSHA:
            "The pull request head SHA is invalid and cannot be used to prepare a managed review workspace."
        case .worktreePathInsideSourceClone:
            "The managed review workspace path would overlap the source clone, so the worktree was not created."
        case .worktreePathOutsideManagedRoot:
            "The managed review workspace path escaped the configured review-workspace root."
        case .fetchedRefMismatch(let expected, let actual):
            "Fetched review ref mismatch: expected \(expected), got \(actual)."
        case .checkedOutHeadMismatch(let expected, let actual):
            "Checked-out review workspace mismatch: expected \(expected), got \(actual)."
        }
    }
}

struct ReviewWorkspacePaths {
    let root: URL

    var metadataFileURL: URL {
        root.appendingPathComponent("metadata.json", isDirectory: false)
    }

    func worktreePath(for pullRequest: ResolvedPullRequest) throws -> URL {
        guard pullRequest.headSHA.range(of: #"^[0-9a-fA-F]{7,40}$"#, options: .regularExpression) != nil else {
            throw ReviewWorkspaceLifecycleError.invalidHeadSHA
        }

        let shortSHA = String(pullRequest.headSHA.prefix(12)).lowercased()
        let candidate =
            root
            .appendingPathComponent(pullRequest.base.host, isDirectory: true)
            .appendingPathComponent(pullRequest.base.owner, isDirectory: true)
            .appendingPathComponent(pullRequest.base.name, isDirectory: true)
            .appendingPathComponent("pr-\(pullRequest.number)-\(shortSHA)", isDirectory: true)
            .standardizedFileURL

        let managedRoot = root.standardizedFileURL
        guard Self.isDescendant(candidate, of: managedRoot) else {
            throw ReviewWorkspaceLifecycleError.worktreePathOutsideManagedRoot
        }
        let sourceClone = pullRequest.localClonePath.standardizedFileURL
        guard !Self.isDescendant(candidate, of: sourceClone) else {
            throw ReviewWorkspaceLifecycleError.worktreePathInsideSourceClone
        }
        return candidate
    }

    private static func isDescendant(_ candidate: URL, of parent: URL) -> Bool {
        let parentPath = parent.path.hasSuffix("/") ? parent.path : parent.path + "/"
        let candidatePath = candidate.path
        return candidatePath == parent.path || candidatePath.hasPrefix(parentPath)
    }
}

struct ReviewWorkspaceMetadataStore {
    let paths: ReviewWorkspacePaths
    let fileManager: FileManager

    init(paths: ReviewWorkspacePaths, fileManager: FileManager = .default) {
        self.paths = paths
        self.fileManager = fileManager
    }

    func load() throws -> [ReviewWorkspaceRecord] {
        let url = paths.metadataFileURL
        guard fileManager.fileExists(atPath: url.path) else { return [] }
        let data = try Data(contentsOf: url)
        return try JSONDecoder.reviewWorkspaceDecoder.decode([ReviewWorkspaceRecord].self, from: data)
    }

    func save(_ records: [ReviewWorkspaceRecord]) throws {
        let directory = paths.root
        try fileManager.createDirectory(at: directory, withIntermediateDirectories: true)
        let data = try JSONEncoder.reviewWorkspaceEncoder.encode(records)
        try data.write(to: paths.metadataFileURL, options: .atomic)
    }

    func upsert(_ record: ReviewWorkspaceRecord) throws {
        var records = try load()
        if let index = records.firstIndex(where: { $0.id == record.id }) {
            records[index] = record
        } else {
            records.append(record)
        }
        try save(records)
    }

    func remove(workspaceID: UUID) throws {
        let records = try load().filter { $0.id != workspaceID }
        try save(records)
    }
}

extension JSONEncoder {
    fileprivate static var reviewWorkspaceEncoder: JSONEncoder {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.prettyPrinted, .sortedKeys]
        encoder.dateEncodingStrategy = .iso8601
        return encoder
    }
}

extension JSONDecoder {
    fileprivate static var reviewWorkspaceDecoder: JSONDecoder {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        return decoder
    }
}

struct ReviewWorkspaceGitService {
    let runner: CommandRunning
    let git: URL
    let fileManager: FileManager
    let paths: ReviewWorkspacePaths

    init(
        runner: CommandRunning = ProcessCommandRunner(),
        git: URL,
        fileManager: FileManager = .default,
        paths: ReviewWorkspacePaths
    ) {
        self.runner = runner
        self.git = git
        self.fileManager = fileManager
        self.paths = paths
    }

    func privateRef(for pullRequest: ResolvedPullRequest) -> String {
        "refs/gh-orbit/review-worktrees/\(pullRequest.base.host)/\(pullRequest.base.owner)/\(pullRequest.base.name)/pr-\(pullRequest.number)/\(pullRequest.headSHA.lowercased())"
    }

    func createWorkspace(
        for pullRequest: ResolvedPullRequest,
        workspaceID: UUID = UUID(),
        now: Date = Date()
    ) throws -> ReviewWorkspaceRecord {
        let worktreePath = try paths.worktreePath(for: pullRequest)
        try fileManager.createDirectory(at: paths.root, withIntermediateDirectories: true)

        let localRef = privateRef(for: pullRequest)
        try materializePrivateRef(for: pullRequest, localRef: localRef)

        let fetchedSHA = try resolveRevision(localRef, workingDirectory: pullRequest.localClonePath)
        guard fetchedSHA == pullRequest.headSHA else {
            throw ReviewWorkspaceLifecycleError.fetchedRefMismatch(
                expected: pullRequest.headSHA, actual: fetchedSHA)
        }

        _ = try runner.run(
            .init(
                executable: git,
                arguments: ["worktree", "add", "--detach", worktreePath.path, localRef],
                workingDirectory: pullRequest.localClonePath))

        let checkedOutSHA = try resolveRevision("HEAD", workingDirectory: worktreePath)
        guard checkedOutSHA == pullRequest.headSHA else {
            throw ReviewWorkspaceLifecycleError.checkedOutHeadMismatch(
                expected: pullRequest.headSHA, actual: checkedOutSHA)
        }

        return .init(
            id: workspaceID,
            repository: pullRequest.base,
            pullRequestNumber: pullRequest.number,
            pullRequestURL: pullRequest.url,
            sourceClonePath: pullRequest.localClonePath,
            sourceCloneRemoteURL: pullRequest.localCloneRemoteURL,
            headRepository: pullRequest.head,
            headCloneURL: pullRequest.headCloneURL,
            headBranch: pullRequest.headBranch,
            headSHA: pullRequest.headSHA,
            worktreePath: worktreePath,
            createdAt: now,
            updatedAt: now,
            state: .active)
    }

    func cleanupWorkspace(_ record: ReviewWorkspaceRecord, now: Date = Date()) -> ReviewWorkspaceCleanupResult {
        do {
            _ = try runner.run(
                .init(
                    executable: git,
                    arguments: ["worktree", "remove", record.worktreePath.path],
                    workingDirectory: record.sourceClonePath))
            return .removed
        } catch {
            var updated = record
            updated.state = .cleanupRequired
            updated.updatedAt = now
            return .cleanupRequired(updated)
        }
    }

    func reconcile(
        _ records: [ReviewWorkspaceRecord],
        now: Date = Date()
    ) throws -> ReviewWorkspaceReconciliationResult {
        let groupedByClone = Dictionary(grouping: records, by: \.sourceClonePath)
        var reconciledRecords: [ReviewWorkspaceRecord] = []
        var orphanedWorktrees: [OrphanedReviewWorkspace] = []

        for (clonePath, cloneRecords) in groupedByClone {
            let listedPaths = try Set(listManagedWorktrees(sourceClonePath: clonePath))
            let knownPaths = Set(cloneRecords.map(\.worktreePath))

            for record in cloneRecords {
                var updated = record
                if listedPaths.contains(record.worktreePath) {
                    if updated.state == .missing {
                        updated.state = .active
                        updated.updatedAt = now
                    }
                } else if updated.state != .missing {
                    updated.state = .missing
                    updated.updatedAt = now
                }
                reconciledRecords.append(updated)
            }

            let managedRoot = paths.root.standardizedFileURL
            for path in listedPaths.subtracting(knownPaths) where Self.isDescendant(path, of: managedRoot) {
                orphanedWorktrees.append(.init(sourceClonePath: clonePath, worktreePath: path))
            }
        }

        return .init(
            records: reconciledRecords.sorted { $0.worktreePath.path < $1.worktreePath.path },
            orphanedWorktrees: orphanedWorktrees.sorted { $0.worktreePath.path < $1.worktreePath.path })
    }

    private func materializePrivateRef(for pullRequest: ResolvedPullRequest, localRef: String) throws {
        let remote = remoteArgument(for: pullRequest.headCloneURL)
        let exactRefSpec = "\(pullRequest.headSHA):\(localRef)"

        do {
            _ = try runner.run(
                .init(
                    executable: git,
                    arguments: ["fetch", "--no-tags", "--force", remote, exactRefSpec],
                    workingDirectory: pullRequest.localClonePath))
            return
        } catch {
            let branchRefSpec = "+refs/heads/\(pullRequest.headBranch):\(localRef)"
            _ = try runner.run(
                .init(
                    executable: git,
                    arguments: ["fetch", "--no-tags", "--force", remote, branchRefSpec],
                    workingDirectory: pullRequest.localClonePath))
        }
    }

    private func remoteArgument(for url: URL) -> String {
        url.isFileURL ? url.path : url.absoluteString
    }

    private func resolveRevision(_ revision: String, workingDirectory: URL) throws -> String {
        try runner.run(
            .init(
                executable: git,
                arguments: ["rev-parse", revision],
                workingDirectory: workingDirectory)
        ).trimmingCharacters(in: .whitespacesAndNewlines)
    }

    private func listManagedWorktrees(sourceClonePath: URL) throws -> [URL] {
        let output = try runner.run(
            .init(
                executable: git,
                arguments: ["worktree", "list", "--porcelain"],
                workingDirectory: sourceClonePath))

        return
            output
            .split(separator: "\n")
            .compactMap { line -> URL? in
                guard line.hasPrefix("worktree ") else { return nil }
                return URL(fileURLWithPath: String(line.dropFirst("worktree ".count))).standardizedFileURL
            }
    }

    private static func isDescendant(_ candidate: URL, of parent: URL) -> Bool {
        let parentPath = parent.path.hasSuffix("/") ? parent.path : parent.path + "/"
        let candidatePath = candidate.standardizedFileURL.path
        return candidatePath == parent.path || candidatePath.hasPrefix(parentPath)
    }
}
