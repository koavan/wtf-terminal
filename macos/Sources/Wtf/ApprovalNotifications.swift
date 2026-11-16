// wtf's notifications, through the system's notification center as wtf
// (the first one asks you to allow them). #39: a permission prompt's has
// Allow and Deny. Without that permission, or outside an .app, they fall
// back to Go's notify service (osascript), which macOS credits to Script
// Editor and often doesn't show.
import AppKit
import UserNotifications
import WtfCore

@MainActor
final class ApprovalNotifications: NSObject, UNUserNotificationCenterDelegate {
    static let shared = ApprovalNotifications()

    private static let category = "wtf.approval"
    private var ready = false

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
    func post(id: String, session: String, title: String, subtitle: String, body: String) async -> Bool {
        await add(id: id, title: title, subtitle: subtitle, body: body,
                  category: Self.category, info: ["approval": id, "session": session])
    }

    /// Shows a plain notification; clicking it goes to the agent's pane when
    /// there's a session. Falls back to osascript when wtf may not post.
    func show(title: String, subtitle: String, body: String, session: String = "") {
        Task {
            if await add(id: UUID().uuidString, title: title, subtitle: subtitle, body: body,
                         category: nil, info: session.isEmpty ? [:] : ["session": session]) { return }
            try? await Backend.run("notify", "Notify", [title, subtitle, body])
        }
    }

    private func add(id: String, title: String, subtitle: String, body: String,
                     category: String?, info: [String: String]) async -> Bool {
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

    /// Takes an approval's notification away: it was answered.
    func remove(_ id: String) {
        guard ready else { return }
        UNUserNotificationCenter.current().removeDeliveredNotifications(withIdentifiers: [id])
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
        await MainActor.run {
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
