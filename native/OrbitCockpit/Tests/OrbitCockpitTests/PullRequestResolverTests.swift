import Foundation
import Testing

@testable import OrbitCockpit

private final class MockCommandRunner: CommandRunning {
    var invocations: [CommandInvocation] = []
    var results: [String]
    var failureAt: Int? = nil

    init(results: [String], failureAt: Int? = nil) {
        self.results = results
        self.failureAt = failureAt
    }

    func run(_ invocation: CommandInvocation) throws -> String {
        invocations.append(invocation)
        if failureAt == invocations.count { throw CommandRunnerError.failed(exitCode: 1, standardError: "fixture") }
        return results.removeFirst()
    }
}

@Suite("Pull request resolver")
struct PullRequestResolverTests {
    @Test
    func resolvesForkWithStructuredCommands() throws {
        let clone = "/Users/test/src/github.com/acme/orbit"
        let runner = MockCommandRunner(results: [
            "\(clone)\n",
            "git@github.example.com:acme/orbit.git\n",
            """
            {"number":12,"url":"https://github.example.com/acme/orbit/pull/12","headRefName":"feature","headRefOid":"deadbeef","headRepository":{"name":"fork","url":"https://github.example.com/contributor/fork.git","owner":{"login":"contributor"}}}
            """,
        ])
        let ghq = URL(fileURLWithPath: "/usr/local/bin/ghq")
        let git = URL(fileURLWithPath: "/usr/bin/git")
        let gh = URL(fileURLWithPath: "/usr/local/bin/gh")
        let resolver = PullRequestResolver(runner: runner, ghq: ghq, git: git, gh: gh)
        let repository = RepositoryIdentity(host: "github.example.com", owner: "acme", name: "orbit")

        let result = try resolver.resolve(repository: repository, number: 12)

        #expect(result.head == RepositoryIdentity(host: "github.example.com", owner: "contributor", name: "fork"))
        #expect(result.headSHA == "deadbeef")
        #expect(
            runner.invocations[0]
                == .init(
                    executable: ghq, arguments: ["list", "--full-path", "--exact", "github.example.com/acme/orbit"],
                    workingDirectory: nil))
        #expect(
            runner.invocations[1]
                == .init(
                    executable: git, arguments: ["remote", "get-url", "origin"],
                    workingDirectory: URL(fileURLWithPath: clone)))
        #expect(
            runner.invocations[2]
                == .init(
                    executable: gh,
                    arguments: [
                        "pr", "view", "12", "--repo", "github.example.com/acme/orbit", "--json",
                        "number,url,headRefName,headRefOid,headRepository",
                    ], workingDirectory: URL(fileURLWithPath: clone)))
    }

    @Test(arguments: [
        (
            "https://github.example.com/acme/orbit.git",
            RepositoryIdentity(host: "github.example.com", owner: "acme", name: "orbit")
        ),
        (
            "git@github.example.com:acme/orbit.git",
            RepositoryIdentity(host: "github.example.com", owner: "acme", name: "orbit")
        ),
    ])
    func normalizesAcceptedRemoteURLs(value: String, expected: RepositoryIdentity) {
        #expect(PullRequestResolver.remoteIdentity(value) == expected)
    }

    @Test
    func rejectsUnsupportedRemoteURLScheme() {
        #expect(PullRequestResolver.remoteIdentity("ssh://github.com/acme/orbit.git") == nil)
    }

    @Test
    func rejectsMismatchedCloneBeforeQueryingGitHub() {
        let runner = MockCommandRunner(results: ["/tmp/orbit\n", "https://github.com/other/orbit.git\n"])
        let resolver = PullRequestResolver(
            runner: runner, ghq: URL(fileURLWithPath: "/ghq"), git: URL(fileURLWithPath: "/git"),
            gh: URL(fileURLWithPath: "/gh"))

        #expect(throws: PullRequestResolutionError.cloneIdentityMismatch) {
            try resolver.resolve(
                repository: RepositoryIdentity(host: "github.com", owner: "acme", name: "orbit"), number: 1)
        }
        #expect(runner.invocations.count == 2)
    }

    @Test
    func mapsUnavailablePullRequestToDiagnostic() {
        let runner = MockCommandRunner(results: ["/tmp/orbit\n", "https://github.com/acme/orbit.git\n"], failureAt: 3)
        let resolver = PullRequestResolver(
            runner: runner, ghq: URL(fileURLWithPath: "/ghq"), git: URL(fileURLWithPath: "/git"),
            gh: URL(fileURLWithPath: "/gh"))

        #expect(throws: PullRequestResolutionError.inaccessiblePullRequest) {
            try resolver.resolve(
                repository: RepositoryIdentity(host: "github.com", owner: "acme", name: "orbit"), number: 1)
        }
    }

    @Test(arguments: ["", "/tmp/a\n/tmp/b\n"])
    func rejectsMissingOrAmbiguousClone(output: String) {
        let runner = MockCommandRunner(results: [output])
        let resolver = PullRequestResolver(
            runner: runner, ghq: URL(fileURLWithPath: "/ghq"), git: URL(fileURLWithPath: "/git"),
            gh: URL(fileURLWithPath: "/gh"))
        #expect(throws: PullRequestResolutionError.noLocalClone) {
            try resolver.resolve(
                repository: RepositoryIdentity(host: "github.com", owner: "acme", name: "orbit"), number: 1)
        }
    }

    @Test
    func mapsMissingHeadToUnavailableDiagnostic() {
        let runner = MockCommandRunner(results: [
            "/tmp/orbit\n", "https://github.com/acme/orbit.git\n",
            "{\"number\":1,\"url\":\"https://github.com/acme/orbit/pull/1\",\"headRefName\":null,\"headRefOid\":null,\"headRepository\":null}",
        ])
        let resolver = PullRequestResolver(
            runner: runner, ghq: URL(fileURLWithPath: "/ghq"), git: URL(fileURLWithPath: "/git"),
            gh: URL(fileURLWithPath: "/gh"))
        #expect(throws: PullRequestResolutionError.unavailableHead) {
            try resolver.resolve(
                repository: RepositoryIdentity(host: "github.com", owner: "acme", name: "orbit"), number: 1)
        }
    }

    @Test
    func resolvesSameRepositoryHead() throws {
        let runner = MockCommandRunner(results: [
            "/tmp/orbit\n", "https://github.com/acme/orbit.git\n",
            "{\"number\":1,\"url\":\"https://github.com/acme/orbit/pull/1\",\"headRefName\":\"main\",\"headRefOid\":\"abc\",\"headRepository\":{\"name\":\"orbit\",\"url\":\"https://github.com/acme/orbit.git\",\"owner\":{\"login\":\"acme\"}}}",
        ])
        let resolver = PullRequestResolver(
            runner: runner, ghq: URL(fileURLWithPath: "/ghq"), git: URL(fileURLWithPath: "/git"),
            gh: URL(fileURLWithPath: "/gh"))
        let result = try resolver.resolve(
            repository: RepositoryIdentity(host: "github.com", owner: "acme", name: "orbit"), number: 1)
        #expect(result.head == result.base)
    }

    @Test
    func reportsMissingExecutableFromEmptyPath() {
        #expect(throws: PullRequestResolutionError.missingExecutable("ghq")) {
            try PullRequestResolver.production(environment: ["PATH": ""])
        }
    }

    @Test
    func mapsGitRemoteFailureToNotGitRepository() {
        let runner = MockCommandRunner(results: ["/tmp/orbit\n"], failureAt: 2)
        let resolver = PullRequestResolver(
            runner: runner, ghq: URL(fileURLWithPath: "/ghq"), git: URL(fileURLWithPath: "/git"),
            gh: URL(fileURLWithPath: "/gh"))
        #expect(throws: PullRequestResolutionError.notGitRepository) {
            try resolver.resolve(
                repository: RepositoryIdentity(host: "github.com", owner: "acme", name: "orbit"), number: 1)
        }
    }
}
