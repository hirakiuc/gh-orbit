import Foundation

@MainActor
final class ReviewRequestStore: ObservableObject {
    @Published var requests: [NativeReviewRequest]

    init(requests: [NativeReviewRequest]? = nil) {
        self.requests = requests ?? Self.defaultRequests()
    }

    func request(forPaneName paneName: String) -> NativeReviewRequest? {
        requests.first { $0.paneName == paneName }
    }

    private static func defaultRequests() -> [NativeReviewRequest] {
        let repository = RepositoryIdentity(host: "github.com", owner: "hirakiuc", name: "gh-orbit")
        return [
            NativeReviewRequest(
                repository: repository,
                pullRequestNumber: 442,
                title: "Native review-workspace launcher",
                subtitle: "Start the signed-off launcher flow from the native review-request surface."
            ),
            NativeReviewRequest(
                repository: repository,
                pullRequestNumber: 441,
                title: "Native CI watchdog investigation",
                subtitle: "Exercise duplicate prevention and failure routing against a prior native PR."
            ),
        ]
    }
}
