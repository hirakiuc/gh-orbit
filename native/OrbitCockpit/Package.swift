// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "OrbitCockpit",
    platforms: [
        .macOS(.v14)
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
        ),
        .testTarget(
            name: "OrbitCockpitTests",
            dependencies: [
                "OrbitCockpit"
            ],
            path: "Tests/OrbitCockpitTests",
            swiftSettings: [
                .enableUpcomingFeature("StrictConcurrency")
            ]
        )
    ]
)
