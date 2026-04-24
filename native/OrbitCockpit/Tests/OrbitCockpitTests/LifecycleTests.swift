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
        let manager = NativeEngineManager(socketPath: fakeSocket)

        #expect(!manager.isEngineReady)

        // We use a non-existent binary to fail start but we want to test waitForSocket
        // But startEngine guards with supervisor.isRunning
        // Let's test the retry logic by using a binary that does nothing (like true)
        let trueURL = URL(fileURLWithPath: "/usr/bin/true")

        let startTime = Date()
        await manager.startEngine(executable: trueURL)
        let duration = Date().timeIntervalSince(startTime)

        #expect(!manager.isEngineReady)
        // 10 attempts with exponential backoff should take at least a few hundred ms
        #expect(duration > 0.5)
    }
}
