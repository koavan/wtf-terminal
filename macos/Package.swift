// swift-tools-version:6.0
// Native macOS UI for wtf: AppKit + SwiftTerm on the Go backend
// (bin/native/libwtf.a, built from cmd/libwtf by build.sh).
import PackageDescription

let package = Package(
    name: "wtf-native",
    platforms: [.macOS(.v14)],
    dependencies: [
        .package(url: "https://github.com/migueldeicaza/SwiftTerm", exact: "1.20.0"),
    ],
    targets: [
        .systemLibrary(name: "CWtf", path: "Sources/CWtf"),
        // UI-independent logic, tested without the Go backend.
        .target(name: "WtfCore", dependencies: ["SwiftTerm"]),
        .testTarget(
            name: "WtfCoreTests",
            dependencies: ["WtfCore", "SwiftTerm"]
        ),
        .executableTarget(
            name: "wtf",
            dependencies: ["CWtf", "WtfCore", "SwiftTerm"],
            path: "Sources/Wtf",
            linkerSettings: [
                .unsafeFlags(["-L../bin/native"]),
                .linkedFramework("CoreFoundation"),
                .linkedFramework("Security"),
                .linkedFramework("Cocoa"),
                .linkedLibrary("resolv"),
            ]
        ),
    ]
)
