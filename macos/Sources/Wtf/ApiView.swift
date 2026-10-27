import AppKit
import WtfCore

/// An API pane: a request editor (method, URL, parameters, headers, body)
/// over its response. It sits in the pane area like a terminal, so tabs,
/// splits, groups and moving between windows work the same. Requests go out
/// only on Send; credentials in headers are masked until revealed and never
/// saved with the session (Api.forSaving).
@MainActor
final class ApiController: NSObject, NSTextFieldDelegate, NSTextViewDelegate, NSSplitViewDelegate {
    var paneId: Int
    let container = ApiContainer()
    private(set) var request: Api.Request
    private var response: Go.apiclient.Response?
    private var failure: String?
    private var sending = false
    /// Names this pane's request in flight, for Cancel.
    private let flightId = "api-" + UUID().uuidString

    /// The keyboard went into this pane.
    var onFocus: (() -> Void)?
    /// The request was edited: the tab's title and the saved session follow.
    var onChange: (() -> Void)?
    /// Run in Shell: a curl command for a terminal.
    var onRunInShell: ((String) -> Void)?

    private let method = NSPopUpButton()
    private let url = NSTextField()
    private let sendButton = NSButton(title: "Send", target: nil, action: nil)
    private let more = NSPopUpButton(frame: .zero, pullsDown: true)
    private let split = NSSplitView()
    private let requestTabs = NSSegmentedControl(labels: ["Params", "Headers", "Body"], trackingMode: .selectOne, target: nil, action: nil)
    private let params = FieldList(masks: false, noun: "Parameter")
    private let headers = FieldList(masks: true, noun: "Header")
    private let bodyPage = NSView()
    private let body = TextBox(editable: true)
    private let contentType = NSPopUpButton(frame: .zero, pullsDown: true)
    private let status = NSTextField(labelWithString: "")
    private let meta = NSTextField(labelWithString: "")
    private let responseTabs = NSSegmentedControl(labels: ["Body", "Headers"], trackingMode: .selectOne, target: nil, action: nil)
    private let pretty = NSSegmentedControl(labels: ["Pretty", "Raw"], trackingMode: .selectOne, target: nil, action: nil)
    private let copyButton = NSButton(title: "Copy", target: nil, action: nil)
    private let responseText = TextBox(editable: false)
    private let hint = NSTextField(wrappingLabelWithString: "")
    private var lastUrlLength = 0
    private var placedDivider = false

    init(pane: Int, request: Api.Request) {
        paneId = pane
        self.request = request
        super.init()
        build()
        load(request)
        showResponse()
        Settings.shared.observe(self) { $0.applyTheme() }
        Redaction.shared.observe(self) { $0.showResponse() }
        applyTheme()
    }

    // MARK: Building

    private func build() {
        container.controller = self

        method.addItems(withTitles: Api.methods)
        method.target = self
        method.action = #selector(methodChanged)
        method.font = .systemFont(ofSize: 12.5, weight: .semibold)

        url.placeholderString = "https://api.example.com/path — or paste a curl command"
        url.font = Viewer.monoFont(12.5)
        url.delegate = self
        url.lineBreakMode = .byTruncatingTail
        url.cell?.isScrollable = true
        url.cell?.wraps = false

        sendButton.target = self
        sendButton.action = #selector(sendOrCancel)
        sendButton.bezelStyle = .rounded
        sendButton.keyEquivalent = ""
        sendButton.toolTip = "Send the request (⌘↩)"

        more.addItem(withTitle: "")
        more.item(at: 0)?.image = NSImage(systemSymbolName: "ellipsis.circle", accessibilityDescription: "More")
        more.bezelStyle = .texturedRounded
        more.isBordered = false
        (more.cell as? NSPopUpButtonCell)?.arrowPosition = .noArrow
        more.menu?.delegate = self
        more.toolTip = "Copy as curl, Run in Shell, TLS checks…"

        let bar = NSStackView(views: [method, url, sendButton, more])
        bar.orientation = .horizontal
        bar.spacing = 8
        bar.edgeInsets = NSEdgeInsets(top: 10, left: 12, bottom: 8, right: 10)
        method.setContentHuggingPriority(.required, for: .horizontal)
        sendButton.setContentHuggingPriority(.required, for: .horizontal)
        more.setContentHuggingPriority(.required, for: .horizontal)
        url.setContentHuggingPriority(.defaultLow, for: .horizontal)

        // Request: parameters, headers or the body.
        requestTabs.selectedSegment = 0
        requestTabs.target = self
        requestTabs.action = #selector(requestTabChanged)
        requestTabs.segmentStyle = .automatic
        requestTabs.controlSize = .small
        params.onChange = { [weak self] in self?.paramsEdited($0) }
        headers.onChange = { [weak self] in self?.headersEdited($0) }
        body.textView.delegate = self

        for t in ["application/json", "text/plain", "application/x-www-form-urlencoded", "application/xml"] {
            contentType.addItem(withTitle: t)
        }
        contentType.insertItem(withTitle: "Content-Type", at: 0)
        contentType.target = self
        contentType.action = #selector(setContentType(_:))
        contentType.controlSize = .small
        contentType.font = .systemFont(ofSize: 11)
        contentType.toolTip = "Sets the Content-Type header"
        let format = NSButton(title: "Format JSON", target: self, action: #selector(formatBody))
        format.controlSize = .small
        format.bezelStyle = .rounded
        format.font = .systemFont(ofSize: 11)
        let bodyBar = NSStackView(views: [contentType, format, NSView()])
        bodyBar.orientation = .horizontal
        bodyBar.spacing = 8
        pin(bodyPage, [bodyBar, body], gap: 6)

        let requestPage = NSView()
        let requestHead = NSStackView(views: [requestTabs, NSView()])
        requestHead.orientation = .horizontal
        let pages = NSView()
        for v in [params, headers, bodyPage] { fill(pages, with: v) }
        pin(requestPage, [requestHead, pages], gap: 8, insets: NSEdgeInsets(top: 4, left: 12, bottom: 8, right: 12))

        // Response: its status line, then the body or the headers.
        status.font = .systemFont(ofSize: 12.5, weight: .semibold)
        meta.font = .systemFont(ofSize: 11.5)
        meta.textColor = .secondaryLabelColor
        meta.lineBreakMode = .byTruncatingMiddle
        meta.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
        responseTabs.selectedSegment = 0
        responseTabs.target = self
        responseTabs.action = #selector(responseViewChanged)
        responseTabs.controlSize = .small
        pretty.selectedSegment = 0
        pretty.target = self
        pretty.action = #selector(responseViewChanged)
        pretty.controlSize = .small
        pretty.toolTip = "JSON indented, or as it came"
        copyButton.target = self
        copyButton.action = #selector(copyResponse)
        copyButton.controlSize = .small
        copyButton.bezelStyle = .rounded
        copyButton.font = .systemFont(ofSize: 11)
        let statusRow = NSStackView(views: [status, meta, NSView(), responseTabs, pretty, copyButton])
        statusRow.orientation = .horizontal
        statusRow.spacing = 8
        hint.textColor = .secondaryLabelColor
        hint.font = .systemFont(ofSize: 12)
        hint.alignment = .center
        let responsePage = NSView()
        let responseArea = NSView()
        fill(responseArea, with: responseText)
        hint.translatesAutoresizingMaskIntoConstraints = false
        responseArea.addSubview(hint)
        NSLayoutConstraint.activate([
            hint.centerXAnchor.constraint(equalTo: responseArea.centerXAnchor),
            hint.topAnchor.constraint(equalTo: responseArea.topAnchor, constant: 28),
            hint.widthAnchor.constraint(lessThanOrEqualTo: responseArea.widthAnchor, constant: -40),
        ])
        pin(responsePage, [statusRow, responseArea], gap: 6, insets: NSEdgeInsets(top: 8, left: 12, bottom: 8, right: 12))

        split.isVertical = false
        split.dividerStyle = .thin
        split.delegate = self
        split.addArrangedSubview(requestPage)
        split.addArrangedSubview(responsePage)
        split.setHoldingPriority(.defaultLow - 1, forSubviewAt: 0)
        split.setHoldingPriority(.defaultLow, forSubviewAt: 1)

        pin(container, [bar, split], gap: 0)
        requestTabChanged()
    }

    /// Stacks views top to bottom in `parent`, the last one taking the rest.
    private func pin(_ parent: NSView, _ views: [NSView], gap: CGFloat, insets: NSEdgeInsets = NSEdgeInsets()) {
        var above: NSLayoutYAxisAnchor = parent.topAnchor
        for (i, v) in views.enumerated() {
            v.translatesAutoresizingMaskIntoConstraints = false
            parent.addSubview(v)
            NSLayoutConstraint.activate([
                v.leadingAnchor.constraint(equalTo: parent.leadingAnchor, constant: insets.left),
                v.trailingAnchor.constraint(equalTo: parent.trailingAnchor, constant: -insets.right),
                v.topAnchor.constraint(equalTo: above, constant: i == 0 ? insets.top : gap),
            ])
            above = v.bottomAnchor
        }
        if let last = views.last {
            last.bottomAnchor.constraint(equalTo: parent.bottomAnchor, constant: -insets.bottom).isActive = true
        }
    }

    private func fill(_ parent: NSView, with v: NSView) {
        v.translatesAutoresizingMaskIntoConstraints = false
        parent.addSubview(v)
        NSLayoutConstraint.activate([
            v.leadingAnchor.constraint(equalTo: parent.leadingAnchor),
            v.trailingAnchor.constraint(equalTo: parent.trailingAnchor),
            v.topAnchor.constraint(equalTo: parent.topAnchor),
            v.bottomAnchor.constraint(equalTo: parent.bottomAnchor),
        ])
    }

    /// The request part starts at about 40% of the pane.
    func placeDivider() {
        guard !placedDivider, container.bounds.height > 200 else { return }
        placedDivider = true
        split.layoutSubtreeIfNeeded()
        split.setPosition((split.bounds.height * 0.4).rounded(), ofDividerAt: 0)
    }

    func splitView(_ splitView: NSSplitView, constrainMinCoordinate p: CGFloat, ofSubviewAt i: Int) -> CGFloat { 90 }
    func splitView(_ splitView: NSSplitView, constrainMaxCoordinate p: CGFloat, ofSubviewAt i: Int) -> CGFloat {
        splitView.bounds.height - 90
    }

    private func applyTheme() {
        guard let theme = Settings.shared.theme else { return }
        container.layer?.backgroundColor = NSColor(hex: theme.background).cgColor
    }

    // MARK: Request ⇄ views

    /// Shows a request in the editor (a new tab, or an imported curl command).
    private func load(_ r: Api.Request) {
        request = r
        let m = r.method.isEmpty ? "GET" : r.method.uppercased()
        if method.item(withTitle: m) == nil { method.addItem(withTitle: m) }
        method.selectItem(withTitle: m)
        url.stringValue = r.url
        lastUrlLength = r.url.count
        params.set(Api.params(r.url))
        headers.set(r.headers)
        body.textView.string = r.body
        updateCounts()
    }

    private func edited() {
        updateCounts()
        onChange?()
    }

    /// "Headers (2)": how many are sent.
    private func updateCounts() {
        let p = params.fields.filter { !$0.off && !$0.name.isEmpty }.count
        let h = request.headers.filter { !$0.off && !$0.name.isEmpty }.count
        requestTabs.setLabel(p > 0 ? "Params (\(p))" : "Params", forSegment: 0)
        requestTabs.setLabel(h > 0 ? "Headers (\(h))" : "Headers", forSegment: 1)
        requestTabs.setLabel(request.body.isEmpty ? "Body" : "Body •", forSegment: 2)
    }

    @objc private func methodChanged() {
        request.method = method.titleOfSelectedItem ?? "GET"
        edited()
    }

    @objc private func requestTabChanged() {
        let i = requestTabs.selectedSegment
        params.isHidden = i != 0
        headers.isHidden = i != 1
        bodyPage.isHidden = i != 2
    }

    func controlTextDidChange(_ note: Notification) {
        guard (note.object as? NSTextField) === url else { return }
        let text = url.stringValue
        let jumped = text.count - lastUrlLength > 1
        lastUrlLength = text.count
        // A pasted curl command fills in the whole request.
        if jumped, text.trimmingCharacters(in: .whitespaces).hasPrefix("curl ") {
            importCurl(text, fallback: request.url)
            return
        }
        request.url = text
        // Parameters follow the URL; ticked-off ones stay listed.
        params.set(Api.params(text) + params.fields.filter(\.off))
        edited()
    }

    func control(_ control: NSControl, textView: NSTextView, doCommandBy sel: Selector) -> Bool {
        if control === url, sel == #selector(NSResponder.insertNewline(_:)) {
            send()
            return true
        }
        return false
    }

    private func paramsEdited(_ fields: [Api.Field]) {
        request.url = Api.url(request.url, params: fields)
        url.stringValue = request.url
        lastUrlLength = request.url.count
        edited()
    }

    private func headersEdited(_ fields: [Api.Field]) {
        request.headers = fields
        edited()
    }

    func textDidChange(_ note: Notification) {
        guard (note.object as? NSTextView) === body.textView else { return }
        request.body = body.textView.string
        edited()
    }

    @objc private func setContentType(_ sender: NSPopUpButton) {
        guard let t = sender.titleOfSelectedItem, sender.indexOfSelectedItem > 0 else { return }
        var hs = request.headers
        if let i = hs.firstIndex(where: { $0.name.caseInsensitiveCompare("Content-Type") == .orderedSame }) {
            hs[i].value = t
            hs[i].off = false
        } else {
            hs.append(.init(name: "Content-Type", value: t))
        }
        headers.set(hs)
        headersEdited(hs)
        Toast.show("Content-Type: \(t)", in: container)
    }

    @objc private func formatBody() {
        let text = body.textView.string
        guard !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { return }
        Task { @MainActor in
            do {
                let out = try await Services.apiclient.FormatJSON(text)
                guard body.textView.string == text else { return }
                // Through the text system, so ⌘Z undoes it.
                let all = NSRange(location: 0, length: (text as NSString).length)
                if body.textView.shouldChangeText(in: all, replacementString: out) {
                    body.textView.replaceCharacters(in: all, with: out)
                    body.textView.didChangeText()
                }
            } catch {
                Toast.show(error.localizedDescription, in: container)
            }
        }
    }

    /// Reads a curl command into the editor; on failure the URL goes back to `fallback`.
    private func importCurl(_ text: String, fallback: String?) {
        Task { @MainActor in
            do {
                let r = try await Services.apiclient.ParseCurl(text)
                load(r)
                requestTabs.selectedSegment = r.body.isEmpty ? (r.headers.isEmpty ? 0 : 1) : 2
                requestTabChanged()
                edited()
                Toast.show("Filled in from the curl command", in: container)
            } catch {
                if let fallback {
                    url.stringValue = fallback
                    lastUrlLength = fallback.count
                }
                Toast.show("Couldn't read the curl command: \(error.localizedDescription)", in: container)
            }
        }
    }

    // MARK: Sending

    @objc private func sendOrCancel() { sending ? cancel() : send() }

    func send() {
        guard !sending else { return }
        sending = true
        failure = nil
        sendButton.title = "Cancel"
        sendButton.toolTip = "Stop waiting for the response"
        status.stringValue = "Sending…"
        status.textColor = .secondaryLabelColor
        meta.stringValue = ""
        let r = request
        Task { @MainActor in
            do {
                response = try await Services.apiclient.Send(flightId, r)
            } catch {
                response = nil
                failure = error.localizedDescription
            }
            sending = false
            sendButton.title = "Send"
            sendButton.toolTip = "Send the request (⌘↩)"
            showResponse()
        }
    }

    func cancel() {
        Task { try? await Services.apiclient.Cancel(flightId) }
    }

    /// Stops a request still in flight: the pane is closing.
    func close() {
        if sending { cancel() }
    }

    // MARK: Response

    @objc private func responseViewChanged() { showResponse() }

    private func showResponse() {
        let red = Redaction.shared
        guard let res = response else {
            status.stringValue = failure == nil ? "" : "Failed"
            status.textColor = .systemRed
            meta.stringValue = ""
            responseText.isHidden = true
            hint.isHidden = false
            hint.stringValue = failure.map { red.text($0) } ?? "Press Send or ⌘↩ to see the response here."
            hint.textColor = failure == nil ? .secondaryLabelColor : .systemRed
            for c in [responseTabs, pretty, copyButton] as [NSControl] { c.isHidden = true }
            return
        }
        status.stringValue = "\(res.status) \(res.statusText)"
        status.textColor = switch res.status {
        case 200..<300: .systemGreen
        case 300..<400: .systemOrange
        default: .systemRed
        }
        var facts = [Api.duration(ms: res.millis), Api.size(res.size)]
        if !res.proto.isEmpty { facts.append(res.proto) }
        if res.truncated { facts.append("cut off at \(Api.size(res.size))") }
        if request.insecure { facts.append("TLS checks off") }
        // Redirected: where the response came from.
        let asked = request.url.trimmingCharacters(in: .whitespaces)
        if res.url != asked, res.url != "http://" + asked { facts.append("from " + red.text(res.url)) }
        meta.stringValue = facts.joined(separator: " · ")
        meta.toolTip = meta.stringValue
        for c in [responseTabs, copyButton] as [NSControl] { c.isHidden = false }
        let showingBody = responseTabs.selectedSegment == 0
        pretty.isHidden = !showingBody || res.pretty.isEmpty
        let text: String
        if showingBody {
            if res.binary {
                responseText.isHidden = true
                hint.isHidden = false
                hint.textColor = .secondaryLabelColor
                hint.stringValue = "The response isn't text (\(res.contentType.isEmpty ? "no Content-Type" : res.contentType), \(Api.size(res.size))), so it isn't shown."
                copyButton.isHidden = true
                return
            }
            text = !res.pretty.isEmpty && pretty.selectedSegment == 0 ? res.pretty : res.body
        } else {
            text = res.headers.map { "\($0.name): \($0.value)" }.joined(separator: "\n")
        }
        hint.isHidden = !text.isEmpty
        hint.stringValue = "The response has no body."
        hint.textColor = .secondaryLabelColor
        responseText.isHidden = text.isEmpty
        responseText.set(red.text(text))
    }

    /// The response as shown (secrets hidden when Hide Secrets is on).
    @objc private func copyResponse() {
        let text = responseText.textView.string
        guard !text.isEmpty else { return }
        Clipboard.copy(text)
        Toast.show(responseTabs.selectedSegment == 0 ? "Copied the response body" : "Copied the response headers", in: container)
    }

    // MARK: More menu

    private var sendsCredentials: Bool {
        request.headers.contains { !$0.off && Api.isSecret($0.name) && !$0.value.isEmpty } || request.url.contains("@")
    }

    @objc private func copyCurl() {
        let r = request
        Task { @MainActor in
            guard let cmd = try? await Services.apiclient.Curl(r, true) else { return }
            let shown = Redaction.shared.text(cmd)
            if sendsCredentials, shown == cmd {
                Clipboard.copySecret(cmd, what: "the curl command", in: container)
            } else {
                Clipboard.copy(shown)
                Toast.show("Copied the curl command", in: container)
            }
        }
    }

    @objc private func runInShell() {
        guard !request.url.trimmingCharacters(in: .whitespaces).isEmpty else {
            Toast.show("Enter a URL first", in: container)
            return
        }
        let r = request
        Task { @MainActor in
            guard let cmd = try? await Services.apiclient.Curl(r, false) else { return }
            onRunInShell?(cmd)
        }
    }

    @objc private func pasteCurl() {
        guard let text = Clipboard.text?.trimmingCharacters(in: .whitespacesAndNewlines), !text.isEmpty else {
            Toast.show("The clipboard is empty", in: container)
            return
        }
        importCurl(text, fallback: nil)
    }

    @objc private func toggleInsecure() {
        request.insecure.toggle()
        edited()
        Toast.show(request.insecure ? "TLS certificates won't be checked (curl -k)" : "TLS certificates are checked", in: container)
    }

    @objc private func clearResponse() {
        response = nil
        failure = nil
        showResponse()
    }

    /// Self-checks: which request and response pages show.
    func debugShow(request page: Int, response: Int = 0) {
        requestTabs.selectedSegment = page
        requestTabChanged()
        responseTabs.selectedSegment = response
        showResponse()
    }

    var debugSummary: String {
        "\(status.stringValue) | \(meta.stringValue) | \(responseText.textView.string.prefix(60))"
    }

    // MARK: Keyboard

    /// Gives the keyboard to the URL field.
    func focus() {
        container.window?.makeFirstResponder(url)
    }

    /// ⌘F: find in the response, or in the body being edited.
    func find(_ action: NSTextFinder.Action) {
        let target: NSTextView
        if let r = container.window?.firstResponder as? NSTextView, r === body.textView {
            target = body.textView
        } else if !responseText.isHidden {
            target = responseText.textView
            if action == .showFindInterface { container.window?.makeFirstResponder(target) }
        } else {
            return
        }
        let item = NSMenuItem()
        item.tag = action.rawValue
        target.performTextFinderAction(item)
    }
}

extension ApiController: NSMenuDelegate {
    func menuNeedsUpdate(_ menu: NSMenu) {
        while menu.items.count > 1 { menu.removeItem(at: 1) }
        func add(_ title: String, _ action: Selector, on: Bool? = nil) {
            let item = NSMenuItem(title: title, action: action, keyEquivalent: "")
            item.target = self
            if let on { item.state = on ? .on : .off }
            menu.addItem(item)
        }
        add("Copy as curl", #selector(copyCurl))
        add("Run in Shell", #selector(runInShell))
        add("Paste curl Command", #selector(pasteCurl))
        menu.addItem(.separator())
        add("Skip TLS Certificate Checks (-k)", #selector(toggleInsecure), on: request.insecure)
        if response != nil || failure != nil { add("Clear Response", #selector(clearResponse)) }
    }
}

/// The API pane's view. Clicks anywhere in it focus the pane; ⌘↩ sends.
@MainActor
final class ApiContainer: NSView {
    weak var controller: ApiController?

    override init(frame: NSRect) {
        super.init(frame: frame)
        wantsLayer = true
    }

    required init?(coder: NSCoder) { fatalError() }

    override func layout() {
        super.layout()
        controller?.placeDivider()
    }

    override func mouseDown(with event: NSEvent) {
        controller?.onFocus?()
        super.mouseDown(with: event)
    }

    override func performKeyEquivalent(with event: NSEvent) -> Bool {
        let mods = event.modifierFlags.intersection(.deviceIndependentFlagsMask)
        if mods == .command, event.keyCode == 36 || event.keyCode == 76,
           let r = window?.firstResponder as? NSView, r.isDescendant(of: self) {
            controller?.send()
            return true
        }
        return super.performKeyEquivalent(with: event)
    }
}

/// A scrolling monospace text view: the request body or the response.
@MainActor
final class TextBox: NSScrollView {
    let textView: NSTextView

    init(editable: Bool) {
        textView = NSTextView(frame: .zero)
        super.init(frame: .zero)
        hasVerticalScroller = true
        hasHorizontalScroller = false
        autohidesScrollers = true
        borderType = .noBorder
        drawsBackground = false
        textView.isEditable = editable
        textView.isSelectable = true
        textView.isRichText = false
        textView.importsGraphics = false
        textView.allowsUndo = editable
        textView.usesFindBar = true
        textView.isIncrementalSearchingEnabled = true
        textView.isAutomaticQuoteSubstitutionEnabled = false
        textView.isAutomaticDashSubstitutionEnabled = false
        textView.isAutomaticTextReplacementEnabled = false
        textView.isAutomaticSpellingCorrectionEnabled = false
        textView.isContinuousSpellCheckingEnabled = false
        textView.isGrammarCheckingEnabled = false
        textView.smartInsertDeleteEnabled = false
        textView.drawsBackground = false
        textView.font = Viewer.monoFont(12)
        textView.textColor = .labelColor
        textView.textContainerInset = NSSize(width: 4, height: 6)
        textView.minSize = .zero
        textView.maxSize = NSSize(width: CGFloat.greatestFiniteMagnitude, height: CGFloat.greatestFiniteMagnitude)
        textView.isVerticallyResizable = true
        textView.isHorizontallyResizable = false
        textView.autoresizingMask = [.width]
        textView.textContainer?.widthTracksTextView = true
        // Big responses lay out only what's on screen.
        textView.layoutManager?.allowsNonContiguousLayout = true
        if editable {
            // A box like the text fields around it, so an empty body shows where to type.
            drawsBackground = true
            backgroundColor = .textBackgroundColor
            borderType = .bezelBorder
        }
        documentView = textView
    }

    required init?(coder: NSCoder) { fatalError() }

    /// Replaces the text (read-only boxes), scrolled to the top.
    func set(_ text: String) {
        guard textView.string != text else { return }
        textView.string = text
        textView.font = Viewer.monoFont(12)
        textView.textColor = .labelColor
        textView.scrollToBeginningOfDocument(nil)
    }
}

/// Name/value rows with a tick to leave one out: query parameters, headers.
/// Header values that are credentials (Api.isSecret) are masked until the
/// eye is clicked.
@MainActor
final class FieldList: NSView, NSTextFieldDelegate {
    private(set) var fields: [Api.Field] = []
    var onChange: (([Api.Field]) -> Void)?
    private let masks: Bool
    private let noun: String
    private let stack = NSStackView()
    private let scroll = NSScrollView()
    /// Rows whose secret value is shown.
    private var revealed = Set<Int>()

    @MainActor
    private final class Row {
        let view = NSStackView()
        let tick = NSButton(checkboxWithTitle: "", target: nil, action: nil)
        let name = NSTextField()
        let valueSlot = NSView()
        var value: NSTextField = NSTextField()
        let eye = NSButton()
        let remove = NSButton()
        var secret = false
    }
    private var rows: [Row] = []
    /// Always last, so an empty list still has a way to add one.
    private let addButton = NSButton(title: "", target: nil, action: nil)

    init(masks: Bool, noun: String) {
        self.masks = masks
        self.noun = noun
        super.init(frame: .zero)
        addButton.title = "Add \(noun)"
        addButton.target = self
        addButton.action = #selector(addRow)
        addButton.image = NSImage(systemSymbolName: "plus", accessibilityDescription: nil)
        addButton.imagePosition = .imageLeading
        addButton.controlSize = .small
        addButton.bezelStyle = .rounded
        addButton.font = .systemFont(ofSize: 11)
        stack.addArrangedSubview(addButton)
        stack.orientation = .vertical
        stack.alignment = .leading
        stack.spacing = 6
        let doc = FlippedView()
        stack.translatesAutoresizingMaskIntoConstraints = false
        doc.addSubview(stack)
        doc.translatesAutoresizingMaskIntoConstraints = false
        scroll.documentView = doc
        scroll.hasVerticalScroller = true
        scroll.autohidesScrollers = true
        scroll.drawsBackground = false
        scroll.translatesAutoresizingMaskIntoConstraints = false
        addSubview(scroll)
        NSLayoutConstraint.activate([
            scroll.leadingAnchor.constraint(equalTo: leadingAnchor),
            scroll.trailingAnchor.constraint(equalTo: trailingAnchor),
            scroll.topAnchor.constraint(equalTo: topAnchor),
            scroll.bottomAnchor.constraint(equalTo: bottomAnchor),
            doc.leadingAnchor.constraint(equalTo: scroll.contentView.leadingAnchor),
            doc.trailingAnchor.constraint(equalTo: scroll.contentView.trailingAnchor),
            doc.topAnchor.constraint(equalTo: scroll.contentView.topAnchor),
            stack.leadingAnchor.constraint(equalTo: doc.leadingAnchor),
            stack.trailingAnchor.constraint(equalTo: doc.trailingAnchor),
            stack.topAnchor.constraint(equalTo: doc.topAnchor, constant: 2),
            stack.bottomAnchor.constraint(equalTo: doc.bottomAnchor, constant: -4),
        ])
    }

    required init?(coder: NSCoder) { fatalError() }

    /// Shows these fields, rebuilding the rows (none is being typed in).
    func set(_ f: [Api.Field]) {
        if f == fields, rows.count == f.count { return }
        // Typing in a row: keep the rows, update the values that changed.
        if f.count == rows.count, let typing = editingRow {
            fields = f
            for (i, r) in rows.enumerated() where i != typing { fill(r, f[i]) }
            return
        }
        fields = f
        revealed = revealed.filter { $0 < f.count }
        rebuild()
    }

    /// The row whose field has the keyboard.
    private var editingRow: Int? {
        guard let r = window?.firstResponder as? NSView else { return nil }
        return rows.firstIndex { r.isDescendant(of: $0.view) }
    }

    private func rebuild() {
        rows.forEach { $0.view.removeFromSuperview() }
        rows = fields.indices.map { makeRow($0) }
        for (i, r) in rows.enumerated() {
            stack.insertArrangedSubview(r.view, at: i)
            r.view.widthAnchor.constraint(equalTo: stack.widthAnchor).isActive = true
        }
    }

    private func makeRow(_ i: Int) -> Row {
        let r = Row()
        r.tick.target = self
        r.tick.action = #selector(ticked(_:))
        r.tick.tag = i
        r.tick.toolTip = "Send this \(noun.lowercased())"
        r.name.placeholderString = "Name"
        r.name.font = Viewer.monoFont(12)
        r.name.delegate = self
        r.name.tag = i
        r.eye.bezelStyle = .inline
        r.eye.isBordered = false
        r.eye.target = self
        r.eye.action = #selector(toggleReveal(_:))
        r.eye.tag = i
        r.remove.image = NSImage(systemSymbolName: "xmark", accessibilityDescription: "Remove")
        r.remove.bezelStyle = .inline
        r.remove.isBordered = false
        r.remove.target = self
        r.remove.action = #selector(removeRow(_:))
        r.remove.tag = i
        r.remove.toolTip = "Remove"
        r.valueSlot.setContentHuggingPriority(.defaultLow, for: .horizontal)
        r.view.orientation = .horizontal
        r.view.spacing = 6
        for v in [r.tick, r.name, r.valueSlot, r.eye, r.remove] { r.view.addArrangedSubview(v) }
        r.name.widthAnchor.constraint(equalTo: r.view.widthAnchor, multiplier: 0.3).isActive = true
        r.eye.setContentHuggingPriority(.required, for: .horizontal)
        r.remove.setContentHuggingPriority(.required, for: .horizontal)
        fill(r, fields[i])
        return r
    }

    /// Puts a field's text in a row, masking the value of a credential.
    private func fill(_ r: Row, _ f: Api.Field) {
        let i = r.name.tag
        r.tick.state = f.off ? .off : .on
        if r.name.stringValue != f.name { r.name.stringValue = f.name }
        let secret = masks && Api.isSecret(f.name) && !revealed.contains(i)
        if secret != r.secret || r.value.superview == nil {
            r.secret = secret
            r.value.removeFromSuperview()
            let v = secret ? NSSecureTextField() : NSTextField()
            v.placeholderString = "Value"
            v.font = Viewer.monoFont(12)
            v.delegate = self
            v.tag = i
            v.lineBreakMode = .byTruncatingTail
            v.cell?.isScrollable = true
            v.cell?.wraps = false
            v.translatesAutoresizingMaskIntoConstraints = false
            r.valueSlot.addSubview(v)
            NSLayoutConstraint.activate([
                v.leadingAnchor.constraint(equalTo: r.valueSlot.leadingAnchor),
                v.trailingAnchor.constraint(equalTo: r.valueSlot.trailingAnchor),
                v.topAnchor.constraint(equalTo: r.valueSlot.topAnchor),
                v.bottomAnchor.constraint(equalTo: r.valueSlot.bottomAnchor),
            ])
            r.value = v
        }
        if r.value.stringValue != f.value { r.value.stringValue = f.value }
        let canReveal = masks && Api.isSecret(f.name)
        r.eye.isHidden = !canReveal
        r.eye.image = NSImage(systemSymbolName: revealed.contains(i) ? "eye.slash" : "eye", accessibilityDescription: "Show")
        r.eye.toolTip = revealed.contains(i) ? "Hide the value" : "Show the value"
    }

    func controlTextDidChange(_ note: Notification) {
        guard let f = note.object as? NSTextField, f.tag < rows.count, f.tag < fields.count else { return }
        let i = f.tag, r = rows[i]
        if f === r.name {
            fields[i].name = f.stringValue
            // Naming a header Authorization masks its value from then on.
            fill(r, fields[i])
        } else if f === r.value {
            fields[i].value = f.stringValue
        }
        onChange?(fields)
    }

    @objc private func ticked(_ sender: NSButton) {
        guard sender.tag < fields.count else { return }
        fields[sender.tag].off = sender.state == .off
        onChange?(fields)
    }

    @objc private func toggleReveal(_ sender: NSButton) {
        let i = sender.tag
        guard i < rows.count else { return }
        if revealed.contains(i) { revealed.remove(i) } else { revealed.insert(i) }
        fill(rows[i], fields[i])
    }

    @objc private func removeRow(_ sender: NSButton) {
        guard sender.tag < fields.count else { return }
        fields.remove(at: sender.tag)
        revealed = Set(revealed.compactMap { $0 == sender.tag ? nil : $0 > sender.tag ? $0 - 1 : $0 })
        rebuild()
        onChange?(fields)
    }

    @objc private func addRow() {
        fields.append(Api.Field())
        rebuild()
        onChange?(fields)
        if let r = rows.last { window?.makeFirstResponder(r.name) }
    }
}
