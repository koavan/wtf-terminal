// The Go backend (cmd/libwtf): service calls by name with JSON arguments,
// backend events, and PTY output streams.
import CWtf
import Foundation

struct BackendError: LocalizedError {
    let message: String
    var errorDescription: String? { message }
}

/// A result-less call's result.
struct Nothing: Decodable, Sendable {}

enum Backend {
    // Calls block for as long as the Go method does, so they run here.
    private static let calls = DispatchQueue(label: "wtf.backend.calls", qos: .userInitiated, attributes: .concurrent)
    // Writes to a PTY can block while the shell isn't reading; one queue
    // keeps keystrokes in order.
    private static let writes = DispatchQueue(label: "wtf.backend.writes", qos: .userInteractive)

    @MainActor private static var handlers: [String: [(Data) -> Void]] = [:]

    /// Builds and starts the services. Empty directories mean the defaults.
    @MainActor
    static func start(version: String, commit: String, buildDate: String, stateDir: String, cacheDir: String) throws {
        if let err = wtf_init(version, commit, buildDate, stateDir, cacheDir, onEvent, nil) {
            defer { wtf_free(err) }
            throw BackendError(message: String(cString: err))
        }
    }

    /// Stops the services; shells are hung up.
    static func shutdown() { wtf_shutdown() }

    /// Calls service.method and decodes its result.
    static func call<R: Decodable & Sendable>(
        _ service: String, _ method: String, _ args: [any Encodable] = [], as _: R.Type = R.self
    ) async throws -> R {
        let json = try encode(args)
        let data: Data = await withCheckedContinuation { done in
            calls.async {
                let out = wtf_call(service, method, json)!
                defer { wtf_free(out) }
                done.resume(returning: Data(bytes: out, count: strlen(out)))
            }
        }
        let reply = try JSONDecoder().decode(Reply<R>.self, from: data)
        if let error = reply.error { throw BackendError(message: error) }
        if let result = reply.result { return result }
        if let none = Nothing() as? R { return none }
        // A nil Go slice or map arrives as null: an empty list or dictionary.
        if let empty = [Any]() as? R { return empty }
        if let empty = [String: Any]() as? R { return empty }
        throw BackendError(message: "\(service).\(method) returned nothing")
    }

    /// Calls service.method on this thread, blocking: only for quick calls
    /// that must finish now (saving the session while quitting).
    static func runNow(_ service: String, _ method: String, _ args: [any Encodable] = []) throws {
        let out = wtf_call(service, method, try encode(args))!
        defer { wtf_free(out) }
        let reply = try JSONDecoder().decode(Reply<Nothing>.self, from: Data(bytes: out, count: strlen(out)))
        if let error = reply.error { throw BackendError(message: error) }
    }

    /// Calls service.method for its effect.
    static func run(_ service: String, _ method: String, _ args: [any Encodable] = []) async throws {
        _ = try await call(service, method, args, as: Nothing.self)
    }

    private struct Reply<R: Decodable>: Decodable {
        let result: R?
        let error: String?
    }

    private static func encode(_ args: [any Encodable]) throws -> String {
        let encoder = JSONEncoder()
        let parts = try args.map { String(decoding: try encoder.encode($0), as: UTF8.self) }
        return "[" + parts.joined(separator: ",") + "]"
    }

    /// Runs handler on the main thread for every event with this name.
    @MainActor
    static func on<T: Decodable>(_ name: String, _: T.Type, _ handler: @escaping (T) -> Void) {
        handlers[name, default: []].append { data in
            do { handler(try JSONDecoder().decode(T.self, from: data)) } catch {
                NSLog("event \(name): \(error)")
            }
        }
    }

    // Arrives on a Go thread; the strings are only valid during the call.
    private static let onEvent: wtf_event_fn = { _, name, json in
        guard let name, let json else { return }
        let event = String(cString: name)
        let data = Data(bytes: json, count: strlen(json))
        DispatchQueue.main.async {
            MainActor.assumeIsolated { handlers[event]?.forEach { $0(data) } }
        }
    }

    /// Streams a session's output to sink until the shell exits.
    @MainActor
    static func attach(_ id: String, to sink: OutputSink) throws {
        // Retained until the stream ends (the len -1 callback).
        let ctx = Unmanaged.passRetained(SinkBox(sink)).toOpaque()
        if let err = wtf_attach(id, onOutput, ctx) {
            defer { wtf_free(err) }
            Unmanaged<SinkBox>.fromOpaque(ctx).release()
            throw BackendError(message: String(cString: err))
        }
    }

    // Arrives on a Go thread. Synchronous: Go sends nothing more for this
    // session until the frame is handed over, which is the backpressure.
    private static let onOutput: wtf_output_fn = { ctx, buf, len in
        guard let ctx else { return }
        let box = Unmanaged<SinkBox>.fromOpaque(ctx)
        guard len >= 0, let buf else {
            box.release()
            return
        }
        let bytes = [UInt8](UnsafeRawBufferPointer(start: buf, count: Int(len)))
        let sink = box.takeUnretainedValue().sink
        DispatchQueue.main.sync { MainActor.assumeIsolated { sink.output(bytes[...]) } }
    }

    /// Sends keyboard input to a session.
    static func write(_ id: String, _ data: ArraySlice<UInt8>) {
        let bytes = Array(data)
        writes.async {
            bytes.withUnsafeBufferPointer { p in
                p.withMemoryRebound(to: CChar.self) {
                    wtf_write(id, UnsafeMutablePointer(mutating: $0.baseAddress), Int64(bytes.count))
                }
            }
        }
    }

    /// Tells the backend whether a window is in front; polling pauses while
    /// none is.
    static func setFocused(window: String, focused: Bool) { wtf_set_focused(window, focused ? 1 : 0, 0) }

    static func forget(window: String) { wtf_set_focused(window, 0, 1) }
}

@MainActor
protocol OutputSink: AnyObject, Sendable {
    func output(_ bytes: ArraySlice<UInt8>)
}

private final class SinkBox: @unchecked Sendable {
    // Only used on the main thread (see onOutput).
    let sink: OutputSink
    init(_ sink: OutputSink) { self.sink = sink }
}
