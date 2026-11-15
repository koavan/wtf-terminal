// #39: a permission prompt's notification with Allow and Deny. wtf's other
// notifications go through osascript (Go's notify service), which can't have
// buttons; this one uses the system's notification center, so the first one
// asks you to allow wtf's notifications. Without that permission it falls
// back to the plain notification.
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
        content.categoryIdentifier = Self.category
        content.userInfo = ["approval": id, "session": session]
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
        guard let id = info["approval"] as? String, let session = info["session"] as? String else { return }
        let action = response.actionIdentifier
        await MainActor.run {
            switch action {
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
