// swift-tools-version:6.0
// Native macOS UI for wtf. M0 spike: one window, one SwiftTerm terminal,
// shell sessions from the Go backend (bin/native/libwtf.a, see cmd/libwtf).
import PackageDescription

let package = Package(
    name: "wtf-native",
    platforms: [.macOS(.v13)],
    dependencies: [
        .package(url: "https://github.com/migueldeicaza/SwiftTerm", exact: "1.20.0"),
    ],
    targets: [
        .systemLibrary(name: "CWtf", path: "Sources/CWtf"),
        .executableTarget(
            name: "WtfSpike",
            dependencies: ["CWtf", "SwiftTerm"],
            linkerSettings: [
                .unsafeFlags(["-L../bin/native"]),
                .linkedFramework("CoreFoundation"),
                .linkedFramework("Security"),
                .linkedFramework("Cocoa"),
                .linkedFramework("WebKit"),
                .linkedFramework("UniformTypeIdentifiers"),
                .linkedLibrary("resolv"),
            ]
        ),
    ]
)
