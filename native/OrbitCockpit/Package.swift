// swift-tools-version: 5.9
import PackageDescription

let package = Package(
    name: "OrbitCockpit",
    platforms: [
        .macOS(.v13)
    ],
    products: [
        .executable(name: "OrbitCockpit", targets: ["OrbitCockpit"])
    ],
    dependencies: [
        .package(url: "https://github.com/migueldeicaza/SwiftTerm.git", from: "1.0.0")
    ],
    targets: [
        .executableTarget(
            name: "OrbitCockpit",
            dependencies: [
                "SwiftTerm"
            ],
            path: "Sources/OrbitCockpit",
            resources: [
                .copy("../../Resources")
            ],
            swiftSettings: [
                .enableUpcomingFeature("StrictConcurrency")
            ]
        )
    ]
)
