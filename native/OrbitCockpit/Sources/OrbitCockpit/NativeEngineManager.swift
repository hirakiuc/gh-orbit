import Combine
import Darwin
import Foundation

enum EngineOwnershipState: Equatable {
    case idle
    case reused
    case spawningOwned
    case ownedReady
    case ownedFailed
}

enum EngineStartupResult: Equatable {
    case reused
    case ownedReady
    case failed(String)
}

private let defaultProbeTimeoutNS: UInt64 = 250_000_000

struct ProbeOutcome: Sendable {
    let success: Bool
    let detail: String

    static func ok(_ detail: String) -> ProbeOutcome {
        ProbeOutcome(success: true, detail: detail)
    }

    static func failed(_ detail: String) -> ProbeOutcome {
        ProbeOutcome(success: false, detail: detail)
    }
}

protocol EngineProbing: Sendable {
    func probe(
        socketPath: String,
        phase: String
    ) async -> ProbeOutcome
}

struct MCPInitializeProbe: EngineProbing {
    private struct RequestEnvelope: Encodable {
        let jsonrpc = "2.0"
        let id: Int
        let method: String
        let params: InitializeParams
    }

    private struct InitializeParams: Encodable {
        let protocolVersion: String
        let capabilities = ClientCapabilities()
        let clientInfo: ClientInfo
    }

    private struct ClientCapabilities: Encodable {}

    private struct ClientInfo: Encodable {
        let name: String
        let version: String
    }

    private struct ResponseEnvelope: Decodable {
        let jsonrpc: String?
        let id: Int?
        let result: ResultPayload?
        let error: ErrorPayload?
    }

    private struct ResultPayload: Decodable {
        let protocolVersion: String?
    }

    private struct ErrorPayload: Decodable {
        let code: Int
        let message: String
    }

    private let protocolVersion = "2025-11-25"
    private let ioTimeoutMS: Int = 200

    func probe(
        socketPath: String,
        phase: String
    ) async -> ProbeOutcome {
        let pathBytes = socketPath.utf8CString
        let maxLength = MemoryLayout<sockaddr_un>.size - MemoryLayout<sa_family_t>.size
        if pathBytes.count >= maxLength {
            return .failed("[\(phase)] socket path too long for UDS probe")
        }

        return await Task.detached(priority: .utility) {
            Self.validate(socketPath: socketPath, protocolVersion: protocolVersion, ioTimeoutMS: ioTimeoutMS)
        }.value
    }

    nonisolated private static func validate(socketPath: String, protocolVersion: String, ioTimeoutMS: Int) -> ProbeOutcome {
        let fd = socket(AF_UNIX, SOCK_STREAM, 0)
        if fd < 0 {
            return .failed("socket() failed: errno=\(errno)")
        }
        defer { close(fd) }

        var timeout = timeval(tv_sec: 0, tv_usec: Int32(ioTimeoutMS * 1000))
        _ = withUnsafePointer(to: &timeout) {
            setsockopt(fd, SOL_SOCKET, SO_RCVTIMEO, $0, socklen_t(MemoryLayout<timeval>.size))
        }
        _ = withUnsafePointer(to: &timeout) {
            setsockopt(fd, SOL_SOCKET, SO_SNDTIMEO, $0, socklen_t(MemoryLayout<timeval>.size))
        }

        var addr = sockaddr_un()
        addr.sun_family = sa_family_t(AF_UNIX)

        let pathBytes = socketPath.utf8CString
        let maxLength = MemoryLayout.size(ofValue: addr.sun_path)
        withUnsafeMutablePointer(to: &addr.sun_path) { ptr in
            ptr.withMemoryRebound(to: CChar.self, capacity: maxLength) { cString in
                for index in 0..<pathBytes.count {
                    cString[index] = pathBytes[index]
                }
            }
        }

        let addrLength = socklen_t(MemoryLayout<sa_family_t>.size + pathBytes.count)
        let connectResult = withUnsafePointer(to: &addr) {
            $0.withMemoryRebound(to: sockaddr.self, capacity: 1) {
                connect(fd, $0, addrLength)
            }
        }
        if connectResult != 0 {
            return .failed("connect() failed: errno=\(errno)")
        }

        let payload = RequestEnvelope(
            id: 1,
            method: "initialize",
            params: InitializeParams(
                protocolVersion: protocolVersion,
                clientInfo: ClientInfo(name: "orbit-cockpit", version: "1.0")
            )
        )

        let encoder = JSONEncoder()
        guard var data = try? encoder.encode(payload) else {
            return .failed("failed to encode initialize payload")
        }
        data.append(0x0A)

        let writeResult = data.withUnsafeBytes {
            Darwin.write(fd, $0.baseAddress, $0.count)
        }
        if writeResult <= 0 {
            return .failed("write() failed: errno=\(errno)")
        }

        var buffer = [UInt8](repeating: 0, count: 4096)
        let readCount = Darwin.read(fd, &buffer, buffer.count)
        if readCount <= 0 {
            return .failed("read() failed or timed out: errno=\(errno)")
        }

        let responseBytes = buffer.prefix(Int(readCount)).prefix { $0 != 0x0A }
        guard let responseData = String(bytes: responseBytes, encoding: .utf8)?.data(using: .utf8),
            let response = try? JSONDecoder().decode(ResponseEnvelope.self, from: responseData)
        else {
            let snippet = String(bytes: responseBytes.prefix(120), encoding: .utf8) ?? "<non-utf8>"
            return .failed("failed to decode initialize response: \(snippet)")
        }

        if let error = response.error {
            return .failed("initialize returned error: code=\(error.code) message=\(error.message)")
        }

        guard response.id == 1 else {
            return .failed("initialize returned unexpected id: \(response.id.map(String.init) ?? "nil")")
        }
        guard let negotiatedVersion = response.result?.protocolVersion, !negotiatedVersion.isEmpty else {
            return .failed("initialize response missing protocolVersion")
        }

        return .ok("initialize succeeded with protocolVersion=\(negotiatedVersion)")
    }
}

/// NativeEngineManager manages the persistent gh-orbit engine process.
@MainActor
class NativeEngineManager: ObservableObject {
    @Published var isEngineReady: Bool = false
    @Published var ownershipState: EngineOwnershipState = .idle

    private let engineSupervisor: EngineProcessSupervising
    private let probe: any EngineProbing
    private let socketPath: String

    private let maxAttempts: Int
    private let baseDelayNS: UInt64
    private let probeTimeoutNS: UInt64
    private var startupTask: Task<EngineStartupResult, Never>?

    init(
        socketPath: String? = nil,
        maxAttempts: Int = 10,
        baseDelayNS: UInt64 = 50_000_000,
        probeTimeoutNS: UInt64 = defaultProbeTimeoutNS,
        engineSupervisor: EngineProcessSupervising? = nil,
        probe: any EngineProbing = MCPInitializeProbe(),
        onLog: ((String, LogLevel) -> Void)? = nil
    ) {
        self.maxAttempts = maxAttempts
        self.baseDelayNS = baseDelayNS
        self.probeTimeoutNS = probeTimeoutNS
        self.probe = probe
        self.engineSupervisor = engineSupervisor ?? ProcessSupervisor()

        if let socketPath = socketPath {
            self.socketPath = socketPath
            onLog?("Using explicit socket path: \(self.socketPath)", .debug)
        } else {
            // Resolve socket path to standard XDG location
            let home = FileManager.default.homeDirectoryForCurrentUser.path
            let runtimeDir =
                ProcessInfo.processInfo.environment["XDG_RUNTIME_DIR"]
                ?? (home + "/.local/run/gh-orbit")
            self.socketPath = runtimeDir + "/engine.sock"
            onLog?("Resolved engine socket path: \(self.socketPath)", .debug)
        }

        // Set the supervisor's logging closure
        self.engineSupervisor.onLog = { message, level in
            onLog?(message, level)
        }
    }

    func startEngine(executable: URL, environment: [String: String]? = nil) async -> EngineStartupResult {
        if let startupTask {
            return await startupTask.value
        }

        if ownershipState == .reused || ownershipState == .ownedReady {
            return ownershipState == .reused ? .reused : .ownedReady
        }

        let task = Task<EngineStartupResult, Never> {
            self.engineSupervisor.onLog?("Starting engine readiness flow for socket: \(self.socketPath)", .debug)
            if await self.verifyExistingEngine() {
                return .reused
            }

            do {
                self.ownershipState = .spawningOwned
                self.isEngineReady = false
                self.engineSupervisor.onLog?("Starting gh-orbit engine with executable: \(executable.path)", .debug)
                try self.engineSupervisor.start(
                    executable: executable,
                    arguments: ["engine", "--socket", self.socketPath, "--insecure-dev"],
                    environment: environment
                )

                let outcome = await self.waitUntilReadyBounded(maxAttempts: self.maxAttempts, phase: "owned-engine verification")
                if outcome.success {
                    self.ownershipState = .ownedReady
                    self.isEngineReady = true
                    self.engineSupervisor.onLog?("Engine is ready (MCP initialize verified). Detail: \(outcome.detail)", .info)
                    return .ownedReady
                }

                self.engineSupervisor.onLog?("Engine failed to become ready. Detail: \(outcome.detail)", .error)
                self.stopOwnedEngineIfNeeded()
                self.ownershipState = .ownedFailed
                self.isEngineReady = false
                return .failed("Orbit Cockpit could not verify the managed gh-orbit engine.")
            } catch {
                self.stopOwnedEngineIfNeeded()
                self.ownershipState = .ownedFailed
                self.isEngineReady = false
                self.engineSupervisor.onLog?("Failed to start engine: \(error)", .error)
                return .failed("Orbit Cockpit could not start the gh-orbit engine: \(error.localizedDescription)")
            }
        }

        startupTask = task
        let result = await task.value
        if startupTask?.isCancelled == false {
            startupTask = nil
        }
        return result
    }

    private func verifyExistingEngine() async -> Bool {
        let outcome = await waitUntilReadyBounded(maxAttempts: 1, phase: "existing-engine probe")
        if outcome.success {
            ownershipState = .reused
            isEngineReady = true
            engineSupervisor.onLog?("Reusing existing gh-orbit engine after MCP initialize verification. Detail: \(outcome.detail)", .info)
            return true
        }
        engineSupervisor.onLog?("Existing-engine probe did not verify a reusable engine. Detail: \(outcome.detail)", .debug)
        return false
    }

    private func waitUntilReadyBounded(maxAttempts: Int, phase: String) async -> ProbeOutcome {
        let probe = self.probe
        let socketPath = self.socketPath
        let baseDelayNS = self.baseDelayNS
        let probeTimeoutNS = self.probeTimeoutNS

        var lastFailure = ProbeOutcome.failed("[\(phase)] probe did not run")
        for attempt in 1...maxAttempts {
            let outcome = await withTaskGroup(of: ProbeOutcome.self) { group in
                group.addTask {
                    await probe.probe(
                        socketPath: socketPath,
                        phase: "\(phase) attempt \(attempt)"
                    )
                }
                group.addTask {
                    try? await Task.sleep(nanoseconds: probeTimeoutNS)
                    return .failed("[\(phase) attempt \(attempt)] timed out after \(probeTimeoutNS / 1_000_000)ms")
                }

                let first = await group.next() ?? .failed("[\(phase) attempt \(attempt)] no probe result")
                group.cancelAll()
                return first
            }

            if outcome.success {
                return outcome
            }

            lastFailure = outcome
            engineSupervisor.onLog?("Probe failure: \(outcome.detail)", .debug)

            if attempt < maxAttempts {
                let delay = UInt64(pow(2.0, Double(attempt)) * Double(baseDelayNS))
                engineSupervisor.onLog?("Retrying probe after \(delay / 1_000_000)ms backoff.", .debug)
                try? await Task.sleep(nanoseconds: delay)
            }
        }

        return lastFailure
    }

    func stopEngine() {
        isEngineReady = false
        switch ownershipState {
        case .spawningOwned, .ownedReady:
            engineSupervisor.onLog?("Stopping gh-orbit engine owned by this Cockpit session.", .info)
            engineSupervisor.stop()
        case .reused:
            engineSupervisor.onLog?("Leaving reused gh-orbit engine running.", .debug)
        case .idle, .ownedFailed:
            break
        }
        ownershipState = .idle
    }

    private func stopOwnedEngineIfNeeded() {
        if engineSupervisor.isRunning {
            engineSupervisor.stop()
        }
    }
}
