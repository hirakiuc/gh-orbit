import Darwin
import Dispatch
import Foundation

struct ReviewWorkspaceStartBridgeRequest: Codable, Equatable {
    let repository: RepositoryIdentity
    let pullRequestNumber: Int

    var nativeReviewRequest: NativeReviewRequest {
        NativeReviewRequest(
            repository: repository,
            pullRequestNumber: pullRequestNumber,
            title: "Start review workspace",
            subtitle: "Requested from the embedded TUI review selection."
        )
    }
}

final class ReviewWorkspaceRequestInbox {
    private let requestDirectoryURL: URL
    private let onLog: ((String, LogLevel) -> Void)?
    private let onRequest: @MainActor (ReviewWorkspaceStartBridgeRequest) -> Void
    private let fileManager: FileManager
    private let queue = DispatchQueue(label: "OrbitCockpit.ReviewWorkspaceRequestInbox")

    private var watcher: DispatchSourceFileSystemObject?
    private var directoryFileDescriptor: Int32 = -1
    private var started = false

    init(
        requestDirectoryURL: URL,
        fileManager: FileManager = .default,
        onLog: ((String, LogLevel) -> Void)? = nil,
        onRequest: @escaping @MainActor (ReviewWorkspaceStartBridgeRequest) -> Void
    ) {
        self.requestDirectoryURL = requestDirectoryURL
        self.fileManager = fileManager
        self.onLog = onLog
        self.onRequest = onRequest
    }

    deinit {
        stop()
    }

    func start() {
        guard !started else { return }
        started = true

        do {
            try prepareRequestDirectory()
            processPendingRequests()
            try startWatcher()
        } catch {
            onLog?("Failed to start review workspace inbox: \(error.localizedDescription)", .error)
        }
    }

    func stop() {
        started = false
        watcher?.cancel()
        watcher = nil
        if directoryFileDescriptor >= 0 {
            close(directoryFileDescriptor)
            directoryFileDescriptor = -1
        }
    }

    func processPendingRequests() {
        queue.sync {
            self.processPendingRequestsOnQueue()
        }
    }

    private func prepareRequestDirectory() throws {
        try fileManager.createDirectory(
            at: requestDirectoryURL,
            withIntermediateDirectories: true,
            attributes: [.posixPermissions: 0o700]
        )
    }

    private func startWatcher() throws {
        let directoryFD = open(requestDirectoryURL.path, O_EVTONLY)
        guard directoryFD >= 0 else {
            throw CocoaError(.fileReadUnknown)
        }

        directoryFileDescriptor = directoryFD
        let watcher = DispatchSource.makeFileSystemObjectSource(
            fileDescriptor: directoryFD,
            eventMask: [.write, .rename],
            queue: queue
        )
        watcher.setEventHandler { [weak self] in
            self?.processPendingRequestsOnQueue()
        }
        watcher.setCancelHandler { [weak self] in
            guard let self else { return }
            if self.directoryFileDescriptor >= 0 {
                close(self.directoryFileDescriptor)
                self.directoryFileDescriptor = -1
            }
        }
        self.watcher = watcher
        watcher.resume()
    }

    private func processPendingRequestsOnQueue() {
        let urls: [URL]
        do {
            urls = try fileManager.contentsOfDirectory(
                at: requestDirectoryURL,
                includingPropertiesForKeys: nil
            ).filter { $0.pathExtension == "json" }.sorted { $0.lastPathComponent < $1.lastPathComponent }
        } catch {
            onLog?("Failed to enumerate review workspace requests: \(error.localizedDescription)", .error)
            return
        }

        for url in urls {
            consumeRequest(at: url)
        }
    }

    private func consumeRequest(at url: URL) {
        let data: Data
        do {
            data = try Data(contentsOf: url)
        } catch {
            onLog?(
                "Failed to read review workspace request \(url.lastPathComponent): \(error.localizedDescription)",
                .error)
            _ = try? fileManager.removeItem(at: url)
            return
        }

        let request: ReviewWorkspaceStartBridgeRequest
        do {
            request = try JSONDecoder().decode(ReviewWorkspaceStartBridgeRequest.self, from: data)
        } catch {
            onLog?(
                "Ignoring malformed review workspace request \(url.lastPathComponent): \(error.localizedDescription)",
                .warning)
            _ = try? fileManager.removeItem(at: url)
            return
        }

        _ = try? fileManager.removeItem(at: url)
        onLog?(
            "Received review workspace request for \(request.repository.owner)/\(request.repository.name)#\(request.pullRequestNumber)",
            .debug
        )
        let onRequest = self.onRequest
        Task { @MainActor in
            onRequest(request)
        }
    }
}
