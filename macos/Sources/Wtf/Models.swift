// Swift mirrors of the Go types the native UI uses so far. The JSON keys
// match the Go struct tags.

/// about.Info
struct AboutInfo: Decodable, Sendable {
    let version: String
    let commit: String
    let buildDate: String
    let goVersion: String
    let os: String
    let arch: String
    let shell: String
    let shellIntegration: Bool
    let configFile: String
    let stateDir: String
    let cacheDir: String
}

/// terminal.ExitEvent (event terminal:exit)
struct TerminalExit: Decodable, Sendable {
    let id: String
    let code: Int
}

/// terminal.Info (event terminal:info)
struct TerminalInfo: Decodable, Sendable {
    let id: String
    let process: String
    let cwd: String
    let isShell: Bool
}
