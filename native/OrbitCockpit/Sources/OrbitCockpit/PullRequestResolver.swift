import Foundation

struct RepositoryIdentity: Codable, Equatable {
    let host: String
    let owner: String
    let name: String

    init(host: String, owner: String, name: String) {
        self.host = host.lowercased()
        self.owner = owner
        self.name = name
    }
}

struct ResolvedPullRequest: Equatable {
    let base: RepositoryIdentity
    let localClonePath: URL
    let localCloneRemoteURL: String
    let number: Int
    let url: URL
    let head: RepositoryIdentity
    let headCloneURL: URL
    let headBranch: String
    let headSHA: String
}

struct CommandInvocation: Equatable {
    let executable: URL
    let arguments: [String]
    let workingDirectory: URL?
}

protocol CommandRunning {
    func run(_ invocation: CommandInvocation) throws -> String
}

enum PullRequestResolutionError: Error, Equatable {
    case missingExecutable(String)
    case noLocalClone, notGitRepository, unsupportedRemote, cloneIdentityMismatch
    case inaccessiblePullRequest, unavailableHead
}

struct PullRequestResolver {
    let runner: CommandRunning
    let ghq: URL
    let git: URL
    let gh: URL

    static func production(
        runner: CommandRunning = ProcessCommandRunner(),
        environment: [String: String] = ProcessInfo.processInfo.environment,
        fileManager: FileManager = .default
    ) throws -> PullRequestResolver {
        func executable(named name: String) throws -> URL {
            let paths = (environment["PATH"] ?? "").split(separator: ":").map(String.init)
            for path in paths {
                let url = URL(fileURLWithPath: path).appendingPathComponent(name)
                if fileManager.isExecutableFile(atPath: url.path) { return url }
            }
            throw PullRequestResolutionError.missingExecutable(name)
        }
        return try .init(
            runner: runner, ghq: executable(named: "ghq"), git: executable(named: "git"), gh: executable(named: "gh"))
    }

    func resolve(repository: RepositoryIdentity, number: Int) throws -> ResolvedPullRequest {
        let query = "\(repository.host)/\(repository.owner)/\(repository.name)"
        let cloneOutput: String
        do {
            cloneOutput = try runner.run(
                .init(executable: ghq, arguments: ["list", "--full-path", "--exact", query], workingDirectory: nil))
        } catch { throw PullRequestResolutionError.noLocalClone }
        let paths = cloneOutput.split(whereSeparator: \.isNewline).map(String.init)
        guard paths.count == 1 else { throw PullRequestResolutionError.noLocalClone }
        let clone = URL(fileURLWithPath: paths[0])
        let remote: String
        do {
            remote = try runner.run(
                .init(executable: git, arguments: ["remote", "get-url", "origin"], workingDirectory: clone)
            ).trimmingCharacters(in: .whitespacesAndNewlines)
        } catch { throw PullRequestResolutionError.notGitRepository }
        guard let parsed = Self.remoteIdentity(remote) else { throw PullRequestResolutionError.unsupportedRemote }
        guard parsed == repository else { throw PullRequestResolutionError.cloneIdentityMismatch }
        let fields = "number,url,headRefName,headRefOid,headRepository"
        let output: String
        do {
            output = try runner.run(
                .init(
                    executable: gh, arguments: ["pr", "view", String(number), "--repo", query, "--json", fields],
                    workingDirectory: clone))
        } catch { throw PullRequestResolutionError.inaccessiblePullRequest }
        guard let metadata = try? JSONDecoder().decode(Metadata.self, from: Data(output.utf8)) else {
            throw PullRequestResolutionError.inaccessiblePullRequest
        }
        guard let headRepository = metadata.headRepository, let headURL = headRepository.url,
            let headBranch = metadata.headRefName, let headSHA = metadata.headRefOid,
            let url = URL(string: metadata.url)
        else { throw PullRequestResolutionError.unavailableHead }
        return .init(
            base: repository, localClonePath: clone, localCloneRemoteURL: remote, number: metadata.number, url: url,
            head: .init(host: repository.host, owner: headRepository.owner.login, name: headRepository.name),
            headCloneURL: headURL, headBranch: headBranch, headSHA: headSHA)
    }

    static func remoteIdentity(_ value: String) -> RepositoryIdentity? {
        let normalized = value.hasSuffix(".git") ? String(value.dropLast(4)) : value
        if let url = URL(string: normalized), url.scheme == "https", let host = url.host {
            let parts = url.path.split(separator: "/")
            guard parts.count == 2 else { return nil }
            return .init(host: host, owner: String(parts[0]), name: String(parts[1]))
        }
        let pattern = #/^git@([^:]+):([^/]+)/([^/]+)$/#
        guard let match = normalized.firstMatch(of: pattern) else { return nil }
        return .init(host: String(match.1), owner: String(match.2), name: String(match.3))
    }

    private struct Metadata: Decodable {
        let number: Int
        let url: String
        let headRefName: String?
        let headRefOid: String?
        let headRepository: HeadRepository?
    }
    private struct HeadRepository: Decodable {
        let name: String
        let url: URL?
        let owner: Owner
    }
    private struct Owner: Decodable { let login: String }
}
