import Foundation
import Testing

@testable import OrbitCockpit

@Suite("Review workspace exact-commit lifecycle")
struct ReviewWorkspaceLifecycleTests {
    @Test
    func buildsDeterministicManagedPathOutsideSourceClone() throws {
        let paths = ReviewWorkspacePaths(root: URL(fileURLWithPath: "/tmp/root", isDirectory: true))
        let pullRequest = ResolvedPullRequest(
            base: .init(host: "github.example.com", owner: "acme", name: "orbit"),
            localClonePath: URL(fileURLWithPath: "/tmp/source/orbit", isDirectory: true),
            localCloneRemoteURL: "git@github.example.com:acme/orbit.git",
            number: 42,
            url: URL(string: "https://github.example.com/acme/orbit/pull/42")!,
            head: .init(host: "github.example.com", owner: "contributor", name: "orbit-fork"),
            headCloneURL: URL(fileURLWithPath: "/tmp/remotes/orbit-fork.git", isDirectory: true),
            headBranch: "feature/review-worktree",
            headSHA: "0123456789abcdef0123456789abcdef01234567")

        let worktreePath = try paths.worktreePath(for: pullRequest)

        #expect(
            worktreePath.path
                == "/tmp/root/github.example.com/acme/orbit/pr-42-0123456789ab")
        #expect(!worktreePath.path.hasPrefix("/tmp/source/orbit/"))
    }

    @Test
    func createsDetachedWorktreeAtExactCommit() throws {
        let fixture = try GitFixture()
        let remote = try fixture.createBareRemote(named: "origin.git")
        let seed = try fixture.clone(remote: remote, into: "seed")
        let headSHA = try fixture.commitFile(
            repository: seed, relativePath: "README.md", contents: "hello main\n", message: "initial")
        _ = try fixture.runGit(["push", "origin", "main"], in: seed)
        let localClone = try fixture.clone(remote: remote, into: "local")

        let service = ReviewWorkspaceGitService(
            git: fixture.git,
            paths: .init(root: fixture.root.appendingPathComponent("managed-root", isDirectory: true)))
        let pullRequest = ResolvedPullRequest(
            base: .init(host: "github.example.com", owner: "acme", name: "orbit"),
            localClonePath: localClone,
            localCloneRemoteURL: remote.path,
            number: 7,
            url: URL(string: "https://github.example.com/acme/orbit/pull/7")!,
            head: .init(host: "github.example.com", owner: "acme", name: "orbit"),
            headCloneURL: remote,
            headBranch: "main",
            headSHA: headSHA)

        let workspaceID = try #require(UUID(uuidString: "AAAAAAAA-BBBB-CCCC-DDDD-EEEEEEEEEEEE"))
        let record = try service.createWorkspace(
            for: pullRequest,
            workspaceID: workspaceID,
            now: Date(timeIntervalSince1970: 10))

        let fetchedSHA = try fixture.revParse(service.privateRef(for: pullRequest), in: localClone)
        let worktreeSHA = try fixture.revParse("HEAD", in: record.worktreePath)
        let headState = try fixture.runGit(["branch", "--show-current"], in: record.worktreePath)

        #expect(record.state == .active)
        #expect(record.headSHA == headSHA)
        #expect(fetchedSHA == headSHA)
        #expect(worktreeSHA == headSHA)
        #expect(headState.isEmpty)
    }

    @Test
    func fetchesForkHeadWithoutConfiguredRemoteAndVerifiesExactCommit() throws {
        let fixture = try GitFixture()
        let baseRemote = try fixture.createBareRemote(named: "origin.git")
        let baseSeed = try fixture.clone(remote: baseRemote, into: "base-seed")
        _ = try fixture.commitFile(
            repository: baseSeed, relativePath: "README.md", contents: "base\n", message: "base")
        _ = try fixture.runGit(["push", "origin", "main"], in: baseSeed)

        let forkRemote = try fixture.createBareRemote(named: "fork.git")
        let forkSeed = try fixture.clone(remote: baseRemote, into: "fork-seed")
        _ = try fixture.runGit(["remote", "add", "fork", forkRemote.path], in: forkSeed)
        _ = try fixture.runGit(["checkout", "-b", "feature/review"], in: forkSeed)
        let forkHeadSHA = try fixture.commitFile(
            repository: forkSeed, relativePath: "feature.txt", contents: "fork head\n", message: "feature")
        _ = try fixture.runGit(["push", "fork", "feature/review"], in: forkSeed)

        let localClone = try fixture.clone(remote: baseRemote, into: "local")

        let service = ReviewWorkspaceGitService(
            git: fixture.git,
            paths: .init(root: fixture.root.appendingPathComponent("managed-root", isDirectory: true)))
        let pullRequest = ResolvedPullRequest(
            base: .init(host: "github.example.com", owner: "acme", name: "orbit"),
            localClonePath: localClone,
            localCloneRemoteURL: baseRemote.path,
            number: 12,
            url: URL(string: "https://github.example.com/acme/orbit/pull/12")!,
            head: .init(host: "github.example.com", owner: "contributor", name: "fork"),
            headCloneURL: forkRemote,
            headBranch: "feature/review",
            headSHA: forkHeadSHA)

        let record = try service.createWorkspace(for: pullRequest)

        let fetchedSHA = try fixture.revParse(service.privateRef(for: pullRequest), in: localClone)
        let worktreeSHA = try fixture.revParse("HEAD", in: record.worktreePath)
        let remotes = try fixture.runGit(["remote"], in: localClone)

        #expect(fetchedSHA == forkHeadSHA)
        #expect(worktreeSHA == forkHeadSHA)
        #expect(!remotes.split(whereSeparator: \.isNewline).contains("fork"))
    }

    @Test
    func cleanTerminationRemovesWorktreeAndMetadata() throws {
        let fixture = try GitFixture()
        let remote = try fixture.createBareRemote(named: "origin.git")
        let seed = try fixture.clone(remote: remote, into: "seed")
        let headSHA = try fixture.commitFile(
            repository: seed, relativePath: "README.md", contents: "hello\n", message: "initial")
        _ = try fixture.runGit(["push", "origin", "main"], in: seed)
        let localClone = try fixture.clone(remote: remote, into: "local")

        let paths = ReviewWorkspacePaths(root: fixture.root.appendingPathComponent("managed-root", isDirectory: true))
        let service = ReviewWorkspaceGitService(git: fixture.git, paths: paths)
        let store = ReviewWorkspaceMetadataStore(paths: paths)
        let pullRequest = ResolvedPullRequest(
            base: .init(host: "github.example.com", owner: "acme", name: "orbit"),
            localClonePath: localClone,
            localCloneRemoteURL: remote.path,
            number: 4,
            url: URL(string: "https://github.example.com/acme/orbit/pull/4")!,
            head: .init(host: "github.example.com", owner: "acme", name: "orbit"),
            headCloneURL: remote,
            headBranch: "main",
            headSHA: headSHA)

        let record = try service.createWorkspace(for: pullRequest)
        try store.upsert(record)
        #expect(FileManager.default.fileExists(atPath: record.worktreePath.path))

        let cleanup = service.cleanupWorkspace(record, now: Date(timeIntervalSince1970: 20))
        switch cleanup {
        case .removed:
            try store.remove(workspaceID: record.id)
        case .cleanupRequired:
            Issue.record("expected clean worktree removal")
        }

        #expect(!FileManager.default.fileExists(atPath: record.worktreePath.path))
        #expect(try store.load().isEmpty)
    }

    @Test
    func dirtyTerminationPreservesWorktreeAndTransitionsToCleanupRequired() throws {
        let fixture = try GitFixture()
        let remote = try fixture.createBareRemote(named: "origin.git")
        let seed = try fixture.clone(remote: remote, into: "seed")
        let headSHA = try fixture.commitFile(
            repository: seed, relativePath: "README.md", contents: "hello\n", message: "initial")
        _ = try fixture.runGit(["push", "origin", "main"], in: seed)
        let localClone = try fixture.clone(remote: remote, into: "local")

        let service = ReviewWorkspaceGitService(
            git: fixture.git,
            paths: .init(root: fixture.root.appendingPathComponent("managed-root", isDirectory: true)))
        let pullRequest = ResolvedPullRequest(
            base: .init(host: "github.example.com", owner: "acme", name: "orbit"),
            localClonePath: localClone,
            localCloneRemoteURL: remote.path,
            number: 9,
            url: URL(string: "https://github.example.com/acme/orbit/pull/9")!,
            head: .init(host: "github.example.com", owner: "acme", name: "orbit"),
            headCloneURL: remote,
            headBranch: "main",
            headSHA: headSHA)
        let record = try service.createWorkspace(for: pullRequest)

        let dirtyFile = record.worktreePath.appendingPathComponent("README.md")
        try "modified\n".write(to: dirtyFile, atomically: true, encoding: .utf8)

        let cleanup = service.cleanupWorkspace(record, now: Date(timeIntervalSince1970: 30))

        switch cleanup {
        case .removed:
            Issue.record("expected cleanupRequired for dirty worktree")
        case .cleanupRequired(let updated):
            #expect(updated.state == .cleanupRequired)
            #expect(updated.updatedAt == Date(timeIntervalSince1970: 30))
            #expect(FileManager.default.fileExists(atPath: updated.worktreePath.path))
        }
    }

    @Test
    func reconciliationMarksMissingAndSurfacesOrphanedManagedWorktrees() throws {
        let fixture = try GitFixture()
        let remote = try fixture.createBareRemote(named: "origin.git")
        let seed = try fixture.clone(remote: remote, into: "seed")
        let headSHA = try fixture.commitFile(
            repository: seed, relativePath: "README.md", contents: "hello\n", message: "initial")
        _ = try fixture.runGit(["push", "origin", "main"], in: seed)
        let localClone = try fixture.clone(remote: remote, into: "local")

        let paths = ReviewWorkspacePaths(root: fixture.root.appendingPathComponent("managed-root", isDirectory: true))
        let service = ReviewWorkspaceGitService(git: fixture.git, paths: paths)
        let first = ResolvedPullRequest(
            base: .init(host: "github.example.com", owner: "acme", name: "orbit"),
            localClonePath: localClone,
            localCloneRemoteURL: remote.path,
            number: 1,
            url: URL(string: "https://github.example.com/acme/orbit/pull/1")!,
            head: .init(host: "github.example.com", owner: "acme", name: "orbit"),
            headCloneURL: remote,
            headBranch: "main",
            headSHA: headSHA)
        let second = ResolvedPullRequest(
            base: .init(host: "github.example.com", owner: "acme", name: "orbit"),
            localClonePath: localClone,
            localCloneRemoteURL: remote.path,
            number: 2,
            url: URL(string: "https://github.example.com/acme/orbit/pull/2")!,
            head: .init(host: "github.example.com", owner: "acme", name: "orbit"),
            headCloneURL: remote,
            headBranch: "main",
            headSHA: headSHA)

        let existingRecord = try service.createWorkspace(for: first)
        let missingRecord = try service.createWorkspace(for: second)
        _ = try fixture.runGit(["worktree", "remove", missingRecord.worktreePath.path], in: localClone)

        let orphanPath = paths.root
            .appendingPathComponent("github.example.com", isDirectory: true)
            .appendingPathComponent("acme", isDirectory: true)
            .appendingPathComponent("orbit", isDirectory: true)
            .appendingPathComponent("pr-999-orphaned", isDirectory: true)
        _ = try fixture.runGit(["worktree", "add", "--detach", orphanPath.path, headSHA], in: localClone)

        let result = try service.reconcile(
            [existingRecord, missingRecord],
            now: Date(timeIntervalSince1970: 40))

        let reconciledExisting = try #require(result.records.first { $0.id == existingRecord.id })
        let reconciledMissing = try #require(result.records.first { $0.id == missingRecord.id })

        #expect(reconciledExisting.state == .active)
        #expect(reconciledMissing.state == .missing)
        #expect(reconciledMissing.updatedAt == Date(timeIntervalSince1970: 40))
        #expect(
            result.orphanedWorktrees == [
                .init(sourceClonePath: localClone, worktreePath: orphanPath.standardizedFileURL)
            ])
    }

    @Test
    func metadataStoreRoundTripsPersistedWorkspaceRecords() throws {
        let root = FileManager.default.temporaryDirectory
            .appendingPathComponent(UUID().uuidString, isDirectory: true)
        defer { try? FileManager.default.removeItem(at: root) }

        let paths = ReviewWorkspacePaths(root: root)
        let store = ReviewWorkspaceMetadataStore(paths: paths)
        let workspaceID = try #require(UUID(uuidString: "AAAAAAAA-BBBB-CCCC-DDDD-EEEEEEEEEEEE"))
        let record = ReviewWorkspaceRecord(
            id: workspaceID,
            repository: .init(host: "github.example.com", owner: "acme", name: "orbit"),
            pullRequestNumber: 1,
            pullRequestURL: URL(string: "https://github.example.com/acme/orbit/pull/1")!,
            sourceClonePath: URL(fileURLWithPath: "/tmp/source"),
            sourceCloneRemoteURL: "git@github.example.com:acme/orbit.git",
            headRepository: .init(host: "github.example.com", owner: "acme", name: "orbit"),
            headCloneURL: URL(fileURLWithPath: "/tmp/origin.git"),
            headBranch: "main",
            headSHA: "0123456789abcdef0123456789abcdef01234567",
            worktreePath: URL(fileURLWithPath: "/tmp/worktree"),
            createdAt: Date(timeIntervalSince1970: 1),
            updatedAt: Date(timeIntervalSince1970: 2),
            state: .cleanupRequired)

        try store.upsert(record)
        #expect(try store.load() == [record])

        try store.remove(workspaceID: record.id)
        #expect(try store.load().isEmpty)
    }
}

private struct GitFixture {
    let root: URL
    let git = URL(fileURLWithPath: "/usr/bin/git")
    private let runner = ProcessCommandRunner()
    private let fileManager = FileManager.default

    init() throws {
        let base = fileManager.temporaryDirectory.appendingPathComponent(UUID().uuidString, isDirectory: true)
        try fileManager.createDirectory(at: base, withIntermediateDirectories: true)
        root = base
    }

    func createBareRemote(named name: String) throws -> URL {
        let remote = root.appendingPathComponent(name, isDirectory: true)
        _ = try runGit(["init", "--bare", remote.path], in: root)
        return remote
    }

    func clone(remote: URL, into name: String) throws -> URL {
        let destination = root.appendingPathComponent(name, isDirectory: true)
        _ = try runGit(["clone", remote.path, destination.path], in: root)
        _ = try runGit(["config", "user.name", "Orbit Test"], in: destination)
        _ = try runGit(["config", "user.email", "orbit@example.com"], in: destination)
        return destination
    }

    func commitFile(repository: URL, relativePath: String, contents: String, message: String) throws -> String {
        let fileURL = repository.appendingPathComponent(relativePath, isDirectory: false)
        let parent = fileURL.deletingLastPathComponent()
        try fileManager.createDirectory(at: parent, withIntermediateDirectories: true)
        try contents.write(to: fileURL, atomically: true, encoding: .utf8)
        _ = try runGit(["add", relativePath], in: repository)
        _ = try runGit(["commit", "-m", message], in: repository)
        return try revParse("HEAD", in: repository)
    }

    func revParse(_ revision: String, in repository: URL) throws -> String {
        try runGit(["rev-parse", revision], in: repository).trimmingCharacters(in: .whitespacesAndNewlines)
    }

    func runGit(_ arguments: [String], in repository: URL) throws -> String {
        try runner.run(.init(executable: git, arguments: arguments, workingDirectory: repository))
    }
}
