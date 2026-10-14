import Foundation

public enum Format {
    /// "0.4s", "12s", "3m 5s", "2h 10m". Matches formatDuration in
    /// frontend/src/lib/toast.svelte.ts.
    public static func duration(ms: Double) -> String {
        if ms < 1000 { return String(format: "%.1fs", ms / 1000) }
        let s = Int((ms / 1000).rounded())
        if s < 60 { return "\(s)s" }
        let m = s / 60
        if m < 60 { return "\(m)m \(s % 60)s" }
        return "\(m / 60)h \(m % 60)m"
    }
}
