import Foundation
import Testing

@testable import OrbitCockpit

@MainActor
final class MockSupervisor: EngineProcessSupervising {
    var isRunning = false
    var onLog: ((String, LogLevel) -> Void)?
    var startCalls = 0
    var stopCalls = 0
    var startError: Error?

    func start(executable: URL, arguments: [String], environment: [String: String]?) throws {
        startCalls += 1
        if let startError {
            throw startError
        }
        isRunning = true
    }

    func stop() {
        stopCalls += 1
        isRunning = false
    }
}

actor MockProbe: EngineProbing {
    struct Step {
        let result: Bool
        let delayNS: UInt64
    }

    var steps: [Step]
    private(set) var callCount = 0

    init(results: [Bool]) {
        self.steps = results.map { Step(result: $0, delayNS: 0) }
    }

    init(steps: [Step]) {
        self.steps = steps
    }

    func waitUntilReady(
        socketPath: String,
        maxAttempts: Int,
        baseDelayNS: UInt64
    ) async -> Bool {
        let index = callCount
        callCount += 1
        if index < steps.count {
            let step = steps[index]
            if step.delayNS > 0 {
                try? await Task.sleep(nanoseconds: step.delayNS)
            }
            return step.result
        }
        return false
    }
}

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
        let probe = MockProbe(results: [false, false])
        let supervisor = MockSupervisor()
        let manager = NativeEngineManager(
            socketPath: "/tmp/non_existent_orbit.sock",
            maxAttempts: 5,
            baseDelayNS: 1_000_000,
            probeTimeoutNS: 50_000_000,
            engineSupervisor: supervisor,
            probe: probe
        )

        let result = await manager.startEngine(executable: URL(fileURLWithPath: "/usr/bin/true"))

        #expect(result == .failed("Orbit Cockpit could not verify the managed gh-orbit engine."))
        #expect(!manager.isEngineReady)
        #expect(manager.ownershipState == .ownedFailed)
        #expect(supervisor.startCalls == 1)
        #expect(supervisor.stopCalls == 1)
    }

    @Test("NativeEngineManager reuses healthy external engine")
    func testReusesHealthyEngine() async throws {
        let probe = MockProbe(results: [true])
        let supervisor = MockSupervisor()
        let manager = NativeEngineManager(
            socketPath: "/tmp/reused.sock",
            engineSupervisor: supervisor,
            probe: probe
        )

        let result = await manager.startEngine(executable: URL(fileURLWithPath: "/usr/bin/true"))

        #expect(result == .reused)
        #expect(manager.isEngineReady)
        #expect(manager.ownershipState == .reused)
        #expect(supervisor.startCalls == 0)
    }

    @Test("NativeEngineManager marks owned engine ready after verification")
    func testOwnedEngineReady() async throws {
        let probe = MockProbe(results: [false, true])
        let supervisor = MockSupervisor()
        let manager = NativeEngineManager(
            socketPath: "/tmp/owned.sock",
            engineSupervisor: supervisor,
            probe: probe
        )

        let result = await manager.startEngine(executable: URL(fileURLWithPath: "/usr/bin/true"))

        #expect(result == .ownedReady)
        #expect(manager.isEngineReady)
        #expect(manager.ownershipState == .ownedReady)
        #expect(supervisor.startCalls == 1)
        #expect(supervisor.stopCalls == 0)
    }

    @Test("NativeEngineManager bounds stalled probe handling")
    func testStalledProbeTimesOut() async throws {
        let probe = MockProbe(steps: [
            .init(result: false, delayNS: 1_000_000_000),
            .init(result: false, delayNS: 1_000_000_000),
        ])
        let supervisor = MockSupervisor()
        let manager = NativeEngineManager(
            socketPath: "/tmp/stalled.sock",
            maxAttempts: 5,
            baseDelayNS: 1_000_000,
            probeTimeoutNS: 50_000_000,
            engineSupervisor: supervisor,
            probe: probe
        )

        let start = Date()
        let result = await manager.startEngine(executable: URL(fileURLWithPath: "/usr/bin/true"))
        let duration = Date().timeIntervalSince(start)

        #expect(result == .failed("Orbit Cockpit could not verify the managed gh-orbit engine."))
        #expect(duration < 0.25)
        #expect(supervisor.startCalls == 1)
        #expect(supervisor.stopCalls == 1)
        #expect(manager.ownershipState == .ownedFailed)
    }

    @Test("NativeEngineManager shares inflight startup across concurrent callers")
    func testConcurrentStartSharesSingleStartupTask() async throws {
        let probe = MockProbe(steps: [
            .init(result: false, delayNS: 0),
            .init(result: true, delayNS: 50_000_000),
        ])
        let supervisor = MockSupervisor()
        let manager = NativeEngineManager(
            socketPath: "/tmp/concurrent.sock",
            maxAttempts: 5,
            baseDelayNS: 1_000_000,
            probeTimeoutNS: 500_000_000,
            engineSupervisor: supervisor,
            probe: probe
        )

        async let first = manager.startEngine(executable: URL(fileURLWithPath: "/usr/bin/true"))
        async let second = manager.startEngine(executable: URL(fileURLWithPath: "/usr/bin/true"))
        let firstResult = await first
        let secondResult = await second

        #expect(firstResult == .ownedReady)
        #expect(secondResult == .ownedReady)
        #expect(supervisor.startCalls == 1)
        #expect(await probe.callCount == 2)
        #expect(manager.ownershipState == .ownedReady)
        #expect(manager.isEngineReady)
    }

    @Test("stopEngine leaves reused engine untouched")
    func testStopEngineDoesNotKillReusedEngine() async throws {
        let probe = MockProbe(results: [true])
        let supervisor = MockSupervisor()
        let manager = NativeEngineManager(
            socketPath: "/tmp/reused.sock",
            engineSupervisor: supervisor,
            probe: probe
        )

        _ = await manager.startEngine(executable: URL(fileURLWithPath: "/usr/bin/true"))
        manager.stopEngine()

        #expect(supervisor.stopCalls == 0)
        #expect(manager.ownershipState == .idle)
        #expect(!manager.isEngineReady)
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
