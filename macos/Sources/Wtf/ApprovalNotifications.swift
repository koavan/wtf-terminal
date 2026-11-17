// wtf's notifications, through the system's notification center as wtf
// (the first one asks you to allow them). #39: a permission prompt's has
// Allow and Deny. Without that permission, or outside an .app, they fall
// back to Go's notify service (osascript), which macOS credits to Script
// Editor and often doesn't show.
//
// They don't pile up: an agent has one notification at most, its latest
// (a new one takes the old one's place), taken away once it's out of date
// (Agents.apply) or you look at the agent. A shell's long-command one
// likewise replaces its last.
import AppKit
import UserNotifications
import WtfCore

@MainActor
final class ApprovalNotifications: NSObject, UNUserNotificationCenterDelegate {
    static let shared = ApprovalNotifications()

    private static let category = "wtf.approval"
    private var ready = false
    /// What an agent's one notification says: the state it showed, and the
    /// approval it asks ("" for none); serial tells posts apart.
    private struct Shown {
        var state: AgentState
        var approval: String
        var serial: Int
    }
    /// Agent session → its notification.
    private var shown: [String: Shown] = [:]
    private var serial = 0

    /// Called once at launch: the buttons, and wtf answers their clicks.
    func setUp() {
        guard !ready, Bundle.main.bundleIdentifier != nil, Bundle.main.bundleURL.pathExtension == "app" else { return }
        let c = UNUserNotificationCenter.current()
        c.delegate = self
        let allow = UNNotificationAction(identifier: "allow", title: "Allow", options: [])
        let deny = UNNotificationAction(identifier: "deny", title: "Deny", options: [.destructive])
        c.setNotificationCategories([UNNotificationCategory(identifier: Self.category, actions: [allow, deny],
                                                            intentIdentifiers: [], options: [])])
        ready = true
    }

    /// Posts an approval's notification with Allow and Deny. False when wtf
    /// may not: the caller posts the plain one instead.
    /// It takes the place of the agent's last one.
    func post(id: String, session: String, title: String, subtitle: String, body: String) async -> Bool {
        await put(session, .waiting, approval: id, title: title, subtitle: subtitle, body: body,
                  category: Self.category, info: ["approval": id, "session": session])
    }

    /// Posts a question Claude asks, with its options as buttons (options
    /// empty: none, clicking it goes to the agent). False when wtf may not.
    func postQuestion(id: String, session: String, title: String, subtitle: String, body: String, options: [String]) async -> Bool {
        var category: String?
        if ready, !options.isEmpty {
            // Buttons belong to a category: one for this question, in place
            // of the last question's.
            let c = UNUserNotificationCenter.current()
            let name = Self.questionCategory + id
            let actions = options.prefix(8).enumerated().map { i, o in
                UNNotificationAction(identifier: "option:\(i)", title: o, options: [])
            }
            var all = await c.notificationCategories().filter { !$0.identifier.hasPrefix(Self.questionCategory) }
            all.insert(UNNotificationCategory(identifier: name, actions: actions, intentIdentifiers: [], options: []))
            c.setNotificationCategories(all)
            category = name
        }
        return await put(session, .waiting, approval: id, title: title, subtitle: subtitle, body: body,
                         category: category, info: ["approval": id, "session": session, "options": options])
    }

    private static let questionCategory = "wtf.question."


    /// Shows an agent's plain notification, in place of its last one;
    /// clicking it goes to the agent's pane. Falls back to osascript when
    /// wtf may not post.
    func show(title: String, subtitle: String, body: String, agent session: String, state: AgentState) {
        Task {
            if await put(session, state, approval: "", title: title, subtitle: subtitle, body: body,
                         category: nil, info: ["session": session]) { return }
            try? await Backend.run("notify", "Notify", [title, subtitle, body])
        }
    }

    /// Posts an agent's notification in its last one's place. One cleared
    /// while it was being posted is taken away once it's there.
    private func put(_ session: String, _ state: AgentState, approval: String, title: String, subtitle: String,
                     body: String, category: String?, info: [String: Any]) async -> Bool {
        serial += 1
        let mine = serial
        shown[session] = Shown(state: state, approval: approval, serial: mine)
        let ok = await add(id: Self.agentKey(session), title: title, subtitle: subtitle, body: body, category: category, info: info)
        switch shown[session]?.serial {
        case mine:
            if !ok { shown[session] = nil }
        case nil:
            if ok { UNUserNotificationCenter.current().removeDeliveredNotifications(withIdentifiers: [Self.agentKey(session)]) }
        default:
            break // a newer one took its place
        }
        return ok
    }

    /// Shows a plain notification; one with the same key (a shell's) takes
    /// the last one's place. Falls back to osascript when wtf may not post.
    func show(title: String, subtitle: String, body: String, key: String) {
        Task {
            if await add(id: key, title: title, subtitle: subtitle, body: body, category: nil, info: [:]) { return }
            try? await Backend.run("notify", "Notify", [title, subtitle, body])
        }
    }

    private static func agentKey(_ session: String) -> String { "agent." + session }

    private func add(id: String, title: String, subtitle: String, body: String,
                     category: String?, info: [String: Any]) async -> Bool {
        guard ready else { return false }
        let c = UNUserNotificationCenter.current()
        var status = await c.notificationSettings().authorizationStatus
        if status == .notDetermined {
            _ = try? await c.requestAuthorization(options: [.alert])
            status = await c.notificationSettings().authorizationStatus
        }
        guard status == .authorized || status == .provisional else { return false }
        let content = UNMutableNotificationContent()
        content.title = title
        content.subtitle = subtitle
        content.body = body
        if let category { content.categoryIdentifier = category }
        content.userInfo = info
        do {
            try await c.add(UNNotificationRequest(identifier: id, content: content, trigger: nil))
            return true
        } catch {
            return false
        }
    }

    /// Takes an approval's notification away: it was answered (here, in
    /// the pane, or it went with its agent).
    func remove(approval id: String) {
        guard let session = shown.first(where: { $0.value.approval == id })?.key else { return }
        clear(session)
    }

    /// Takes an agent's notification away unless it still says `state`
    /// (nil: whatever it says).
    func clear(_ session: String, unless state: AgentState? = nil) {
        guard let was = shown[session], was.state != state else { return }
        shown[session] = nil
        UNUserNotificationCenter.current().removeDeliveredNotifications(withIdentifiers: [Self.agentKey(session)])
    }

    nonisolated func userNotificationCenter(_ center: UNUserNotificationCenter, didReceive response: UNNotificationResponse) async {
        let info = response.notification.request.content.userInfo
        let action = response.actionIdentifier
        guard let session = info["session"] as? String else {
            // A command's notification: bring wtf forward.
            await MainActor.run { NSApp.activate(ignoringOtherApps: true) }
            return
        }
        let id = info["approval"] as? String ?? ""
        let options = info["options"] as? [String] ?? []
        await MainActor.run {
            if action.hasPrefix("option:"), let i = Int(action.dropFirst(7)), i < options.count {
                Task {
                    do {
                        try await Services.bridge.AnswerQuestion(id, [options[i]])
                    } catch {
                        Agents.go(to: session)
                        if let w = Agents.find(session)?.0 { Toast.show("Already answered in the pane", in: w.rootView) }
                    }
                }
                return
            }
            switch id.isEmpty ? UNNotificationDefaultActionIdentifier : action {
            case "allow", "deny":
                Task {
                    do {
                        try await Services.bridge.Answer(id, action)
                    } catch {
                        Agents.go(to: session)
                        if let w = Agents.find(session)?.0 { Toast.show("Already answered in the pane", in: w.rootView) }
                    }
                }
            default:
                Agents.go(to: session)
            }
        }
    }

    nonisolated func userNotificationCenter(_ center: UNUserNotificationCenter, willPresent notification: UNNotification) async
        -> UNNotificationPresentationOptions
    {
        // Posted only while you look elsewhere: show it even with wtf in front.
        [.banner, .list]
    }
}
