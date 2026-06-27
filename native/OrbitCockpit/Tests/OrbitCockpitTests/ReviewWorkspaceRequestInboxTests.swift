import Foundation
import Testing

@testable import OrbitCockpit

@Suite("Review workspace request inbox")
@MainActor
struct ReviewWorkspaceRequestInboxTests {
    @Test("Consumes structured request files and forwards them to the native launcher")
    func consumesStructuredRequestFiles() async throws {
        let root = URL(fileURLWithPath: NSTemporaryDirectory(), isDirectory: true)
            .appendingPathComponent(UUID().uuidString, isDirectory: true)
        try FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: root) }

        let request = ReviewWorkspaceStartBridgeRequest(
            repository: .init(host: "github.com", owner: "acme", name: "orbit"),
            pullRequestNumber: 42
        )
        let payload = try JSONEncoder().encode(request)
        let fileURL = root.appendingPathComponent("0001-request.json", isDirectory: false)
        try payload.write(to: fileURL)

        var received: [ReviewWorkspaceStartBridgeRequest] = []
        let inbox = ReviewWorkspaceRequestInbox(
            requestDirectoryURL: root,
            onRequest: { received.append($0) }
        )

        inbox.processPendingRequests()
        try await Task.sleep(nanoseconds: 50_000_000)

        #expect(received == [request])
        #expect(!FileManager.default.fileExists(atPath: fileURL.path))
    }

    @Test("Malformed request files are discarded without invoking the handler")
    func discardsMalformedRequestFiles() async throws {
        let root = URL(fileURLWithPath: NSTemporaryDirectory(), isDirectory: true)
            .appendingPathComponent(UUID().uuidString, isDirectory: true)
        try FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: root) }

        let fileURL = root.appendingPathComponent("0002-request.json", isDirectory: false)
        try Data("not-json".utf8).write(to: fileURL)

        var callCount = 0
        let inbox = ReviewWorkspaceRequestInbox(
            requestDirectoryURL: root,
            onRequest: { _ in callCount += 1 }
        )

        inbox.processPendingRequests()
        try await Task.sleep(nanoseconds: 50_000_000)

        #expect(callCount == 0)
        #expect(!FileManager.default.fileExists(atPath: fileURL.path))
    }
}
