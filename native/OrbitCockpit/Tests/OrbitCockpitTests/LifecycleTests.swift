import Foundation
import Testing

@testable import OrbitCockpit

struct MockFileSystem: FileSystem {
    var exists: Set<String> = []
    var currentPath: String = "/test/project"

    func fileExists(atPath: String) -> Bool {
        return exists.contains(atPath)
    }
    var currentDirectoryPath: String {
        return currentPath
    }
}

@Suite("Process Lifecycle Tests")
@MainActor
struct LifecycleTests {

    @Test("ProcessSupervisor state transitions")
    func testProcessSupervisorTransitions() async throws {
        let supervisor = ProcessSupervisor()

        #expect(!supervisor.isRunning)

        // Use /usr/bin/true for a successful run
        let trueURL = URL(fileURLWithPath: "/usr/bin/true")
        try supervisor.start(executable: trueURL, arguments: [], environment: nil)

        #expect(supervisor.isRunning)

        // Wait for termination (async updates)
        for _ in 0..<20 {
            if !supervisor.isRunning { break }
            try await Task.sleep(nanoseconds: 50_000_000)
        }

        #expect(!supervisor.isRunning)
        #expect(supervisor.exitCode == 0)
    }

    @Test("NativeEngineManager UDS retry logic failure")
    func testUDSRetryLogicFailure() async throws {
        // Use a path that will never exist to test timeout/retry failure
        let fakeSocket = "/tmp/non_existent_orbit_\(UUID().uuidString).sock"
        // Optimize: use 1ms base delay and 5 attempts for fast testing (~60ms total)
        let manager = NativeEngineManager(socketPath: fakeSocket, maxAttempts: 5, baseDelayNS: 1_000_000)

        #expect(!manager.isEngineReady)

        let trueURL = URL(fileURLWithPath: "/usr/bin/true")

        let startTime = Date()
        await manager.startEngine(executable: trueURL)
        let duration = Date().timeIntervalSince(startTime)

        #expect(!manager.isEngineReady)
        // 5 attempts with 1ms base should take at least ~60ms (2+4+8+16+32)
        #expect(duration > 0.05)
    }

    @Test("PathResolver binary discovery logic")
    func testPathResolver() async throws {
        // 1. Test Environment Override
        var fileSystem = MockFileSystem()
        fileSystem.exists.insert("/custom/bin/gh-orbit")
        let env = ["GH_ORBIT_BIN": "/custom/bin/gh-orbit"]

        let url1 = PathResolver.resolveBinary(fileSystem: fileSystem, env: env)
        #expect(url1?.path == "/custom/bin/gh-orbit")

        // 2. Test Project Root Discovery (at depth 0)
        var fileSystem2 = MockFileSystem()
        fileSystem2.currentPath = "/Users/dev/orbit"
        fileSystem2.exists.insert("/Users/dev/orbit/bin/gh-orbit")
        let url2 = PathResolver.resolveBinary(fileSystem: fileSystem2, env: [:])
        #expect(url2?.path == "/Users/dev/orbit/bin/gh-orbit")

        // 3. Test Project Root Discovery (walking up 2 levels)
        var fileSystem3 = MockFileSystem()
        fileSystem3.currentPath = "/Users/dev/orbit/internal/engine"
        fileSystem3.exists.insert("/Users/dev/orbit/bin/gh-orbit")
        let url3 = PathResolver.resolveBinary(fileSystem: fileSystem3, env: [:])
        #expect(url3?.path == "/Users/dev/orbit/bin/gh-orbit")

        // 4. Test Search Limit (6 levels deep should fail)
        var fileSystem4 = MockFileSystem()
        fileSystem4.currentPath = "/Users/dev/orbit/1/2/3/4/5/6"
        fileSystem4.exists.insert("/Users/dev/orbit/bin/gh-orbit")
        let url4 = PathResolver.resolveBinary(fileSystem: fileSystem4, env: [:])
        #expect(url4 == nil)

        // 5. Test Sentinel Stop (stops at go.mod even if binary is higher)
        var fileSystem5 = MockFileSystem()
        fileSystem5.currentPath = "/Users/dev/orbit/subdir"
        fileSystem5.exists.insert("/Users/dev/bin/gh-orbit")  // binary is higher up
        fileSystem5.exists.insert("/Users/dev/orbit/go.mod")  // but project root is here
        let url5 = PathResolver.resolveBinary(fileSystem: fileSystem5, env: [:])
        #expect(url5 == nil)  // Should not find the binary because search stops at go.mod
    }

    @Test("ActivityMonitor aggregation and debouncing")
    func testActivityMonitor() async throws {
        let monitor = ActivityMonitor()

        #expect(monitor.logs.isEmpty)

        monitor.log(component: "[App]", level: .info, message: "Launch started")

        // Wait for debouncer (0.1s interval)
        for _ in 0..<20 {
            if !monitor.logs.isEmpty { break }
            try await Task.sleep(nanoseconds: 100_000_000)
        }

        #expect(monitor.logs.contains(where: { $0.component == "[App]" && $0.message == "Launch started" }))

        // Test multi-source aggregation
        monitor.log(component: "[Engine]", level: .debug, message: "Engine started")

        // Wait for debouncer again
        for _ in 0..<20 {
            if monitor.logs.count > 1 { break }
            try await Task.sleep(nanoseconds: 100_000_000)
        }

        let logs = monitor.getLogs()
        let hasAppLog = logs.contains(where: { $0.component == "[App]" && $0.message == "Launch started" })
        #expect(hasAppLog)

        let hasEngineLog = logs.contains(where: {
            $0.component == "[Engine]" && $0.message == "Engine started" && $0.level == .debug
        })
        #expect(hasEngineLog)
    }

    @Test("Structured log parsing")
    func testStructuredLogParsing() async throws {
        let supervisor = ProcessSupervisor()

        let debugLog = supervisor.parseLogLevel(from: "{\"level\":\"DEBUG\",\"msg\":\"testing\"}", defaultLevel: .error)
        #expect(debugLog == .debug)

        let infoLog = supervisor.parseLogLevel(from: "{\"level\":\"INFO\",\"msg\":\"testing\"}", defaultLevel: .error)
        #expect(infoLog == .info)

        let unknownLog = supervisor.parseLogLevel(from: "just a raw string", defaultLevel: .error)
        #expect(unknownLog == .error)
    }
}
