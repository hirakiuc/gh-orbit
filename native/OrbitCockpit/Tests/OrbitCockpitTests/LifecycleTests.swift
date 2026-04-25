import Foundation
import Testing

@testable import OrbitCockpit

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

    @Test("PathResolver binary discovery")
    func testResolveBinary() async throws {
        let url = PathResolver.resolveBinary()
        if let binURL = url {
            #expect(binURL.path.contains("gh-orbit"))
        }
    }
}
