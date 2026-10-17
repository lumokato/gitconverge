import AppKit
import WebKit

// AppKit owns the menu bar; the Go bridge owns authenticated loopback HTTP and
// worker commands. The only timer below READS status; launchd alone schedules sync.
final class AppDelegate: NSObject, NSApplicationDelegate, WKNavigationDelegate {
    var item: NSStatusItem!
    let menu = NSMenu()
    let status = NSMenuItem(title: "正在读取状态…", action: nil, keyEquivalent: "")
    let schedule = NSMenuItem(title: "自动同步：检测中", action: nil, keyEquivalent: "")
    var runItem: NSMenuItem!
    var pauseItem: NSMenuItem!
    var window: NSWindow?
    var web: WKWebView?
    var bridge: Process?
    var input: FileHandle?
    var output: FileHandle?
    let queue = DispatchQueue(label: "com.gitconverge.bridge")
    var timer: Timer?
    var paused = false
    var version = "unknown"
    var baseURL: URL?
    var busy = false
    let lockFD: Int32 = open((NSHomeDirectory() + "/Library/Caches/com.gitconverge.menubar.lock"), O_CREAT | O_RDWR, 0o600)

    func applicationDidFinishLaunching(_ notification: Notification) {
        // Separate from worker lock. Reopening never creates a second menu/bridge.
        guard lockFD >= 0 && flock(lockFD, LOCK_EX | LOCK_NB) == 0 else { NSApp.terminate(nil); return }
        item = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
        item.button?.image = NSImage(systemSymbolName: "arrow.triangle.2.circlepath", accessibilityDescription: "GitConverge")
        item.button?.image?.isTemplate = true
        item.button?.toolTip = "GitConverge"
        item.menu = menu
        status.isEnabled = false; menu.addItem(status)
        schedule.isEnabled = false; menu.addItem(schedule)
        menu.addItem(.separator())
        add("打开管理界面", #selector(openDashboard))
        runItem = add("立即同步", #selector(runNow))
        add("查看运行日志", #selector(openLogs))
        pauseItem = add("暂停自动同步", #selector(togglePause))
        menu.addItem(.separator())
        add("关于 GitConverge", #selector(about))
        add("退出菜单栏（同步继续）", #selector(quit))
        startBridge()
        refresh()
        // Opt-in bounded UI evidence capture: only this application's window/menu.
        if let index = CommandLine.arguments.firstIndex(of: "--verify-ui"), CommandLine.arguments.count > index + 1 {
            let directory = CommandLine.arguments[index + 1]
            DispatchQueue.main.asyncAfter(deadline: .now() + 2) {
                self.openDashboard()
                DispatchQueue.main.asyncAfter(deadline: .now() + 5) {
                    if let window = self.window { self.capture(window.windowNumber, directory + "/dashboard-private.png") }
                    if let iconWindow = self.item.button?.window { self.capture(iconWindow.windowNumber, directory + "/menubar-icon.png") }
                    let captureTimer = Timer(timeInterval: 2, repeats: false) { _ in
                        self.captureMenu(directory)
                        self.menu.cancelTracking()
                        NSApp.terminate(nil)
                    }
                    RunLoop.main.add(captureTimer, forMode: .common)
                    self.item.button?.performClick(nil)
                }
            }
        }
        timer = Timer.scheduledTimer(withTimeInterval: 4, repeats: true) { [weak self] _ in self?.refresh() }
    }
    func capture(_ id: Int, _ path: String) {
        let process = Process(); process.executableURL = URL(fileURLWithPath: "/usr/sbin/screencapture")
        process.arguments = ["-x", "-o", "-l", String(id), path]
        try? process.run(); process.waitUntilExit()
    }
    func captureMenu(_ directory: String) {
        let windows = CGWindowListCopyWindowInfo([.optionOnScreenOnly], kCGNullWindowID) as? [[String: Any]] ?? []
        for window in windows where window[kCGWindowOwnerPID as String] as? Int == Int(getpid()) {
            let layer = window[kCGWindowLayer as String] as? Int ?? 0
            if layer > 0, let number = window[kCGWindowNumber as String] as? Int { capture(number, directory + "/menu-\(number).png") }
        }
    }
    @discardableResult func add(_ title: String, _ selector: Selector) -> NSMenuItem {
        let entry = NSMenuItem(title: title, action: selector, keyEquivalent: "")
        entry.target = self; menu.addItem(entry); return entry
    }
    func startBridge() {
        let process = Process()
        process.executableURL = Bundle.main.bundleURL.appendingPathComponent("Contents/MacOS/gitconverge-worker")
        process.arguments = ["desktop-bridge"]
        let incoming = Pipe(), outgoing = Pipe()
        process.standardInput = incoming; process.standardOutput = outgoing
        // Do not log URLs/tokens. Diagnostic stderr remains local to launchd.
        process.standardError = FileHandle.nullDevice
        do {
            try process.run(); bridge = process
            input = incoming.fileHandleForWriting; output = outgoing.fileHandleForReading
        } catch { showError("后台管理入口无法启动：\(error.localizedDescription)") }
    }
    func command(_ action: String, _ complete: @escaping ([String: Any]) -> Void) {
        queue.async { [weak self] in
            guard let self = self, self.bridge?.isRunning == true, let input = self.input, let output = self.output else {
                DispatchQueue.main.async { self?.busy = false; self?.showError("管理进程已退出；后台同步独立运行，请重新打开菜单栏。") }; return
            }
            do {
                var data = try JSONSerialization.data(withJSONObject: ["action": action]); data.append(10)
                try input.write(contentsOf: data)
                var line = Data()
                while line.count < 2_000_000 {
                    guard let byte = try output.read(upToCount: 1), !byte.isEmpty else { throw NSError(domain: "bridge", code: 1) }
                    if byte[0] == 10 { break }; line.append(byte)
                }
                let response = try JSONSerialization.jsonObject(with: line) as? [String: Any] ?? [:]
                DispatchQueue.main.async { complete(response) }
            } catch { DispatchQueue.main.async { self.busy = false; self.showError("无法读取管理状态；后台同步未被停止。") } }
        }
    }
    func showError(_ message: String) {
        status.title = "状态不可用 / 错误"
        item.button?.toolTip = message
        runItem?.isEnabled = false
    }
    func refresh() {
        guard !busy else { return }; busy = true
        command("status") { [weak self] response in
            guard let self = self else { return }; self.busy = false
            self.version = response["version"] as? String ?? "unknown"
            if let error = response["error"] as? String { self.showError(error); return }
            let snapshot = response["snapshot"] as? [String: Any] ?? [:]
            let runtime = snapshot["runtime"] as? [String: Any] ?? [:]
            let summary = snapshot["summary"] as? [String: Any] ?? [:]
            let scheduler = response["schedule"] as? [String: Any] ?? [:]
            self.paused = scheduler["paused"] as? Bool ?? false
            let loaded = scheduler["loaded"] as? Bool ?? false
            let active = runtime["syncActive"] as? Bool ?? false
            let attention = summary["needsAttention"] as? Int ?? 0
            let count = summary["repositories"] as? Int ?? 0
            let last = summary["lastCompleted"] as? String
            let latestRun = response["lastRun"] as? [String: Any] ?? [:]
            let failed = latestRun["status"] as? String == "failed"
            let state = active ? "同步中" : (self.paused ? "已暂停自动同步" : (!loaded ? "自动调度未加载" : ((attention > 0 || failed) ? "最近运行有错误" : "空闲（非实时成功）")))
            self.status.title = "\(state) · \(count) 个仓库"
            self.schedule.title = last.map { "最近完成：\($0.prefix(19))" } ?? "尚无完成记录"
            self.pauseItem.title = self.paused ? "恢复自动同步" : "暂停自动同步"
            self.runItem.isEnabled = snapshot["configured"] as? Bool == true && !active
            self.item.button?.toolTip = "GitConverge \(self.version) · \(state)"
        }
    }
    func perform(_ action: String) {
        runItem.isEnabled = false
        command(action) { [weak self] response in
            if let error = response["error"] as? String { self?.showError(error) }
            else { self?.refresh() }
        }
    }
    @objc func runNow() { perform("run") }
    @objc func togglePause() { perform(paused ? "resume" : "pause") }
    @objc func openDashboard() { openPage("open") }
    @objc func openLogs() { openPage("logs") }
    func openPage(_ action: String) {
        command(action) { [weak self] response in
            guard let self = self else { return }
            if let error = response["error"] as? String { self.showError(error); return }
            guard let text = response["url"] as? String, let url = URL(string: text), url.host == "127.0.0.1" else { return }
            self.baseURL = url
            if self.window == nil {
                let web = WKWebView(); web.navigationDelegate = self; self.web = web
                let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 1080, height: 740), styleMask: [.titled, .closable, .miniaturizable, .resizable], backing: .buffered, defer: false)
                window.title = "GitConverge — 本机管理"; window.contentView = web
                window.isReleasedWhenClosed = false; window.center(); self.window = window
            }
            self.web?.load(URLRequest(url: url))
            NSApp.activate(ignoringOtherApps: true); self.window?.makeKeyAndOrderFront(nil)
        }
    }
    func webView(_ webView: WKWebView, decidePolicyFor navigationAction: WKNavigationAction, decisionHandler: @escaping (WKNavigationActionPolicy) -> Void) {
        guard let url = navigationAction.request.url, let base = baseURL,
              url.scheme == base.scheme, url.host == base.host, url.port == base.port else { decisionHandler(.cancel); return }
        decisionHandler(.allow)
    }
    @objc func about() {
        let alert = NSAlert(); alert.messageText = "GitConverge \(version)"
        alert.informativeText = "macOS 菜单栏 + 本机管理界面\n后台同步由独立 LaunchAgent 执行。退出菜单栏不会停止同步。\n此构建没有 Apple Developer ID 签名或公证。\nApache License 2.0"
        alert.runModal()
    }
    @objc func quit() { NSApp.terminate(nil) }
    func applicationWillTerminate(_ notification: Notification) {
        timer?.invalidate(); try? input?.close()
        // EOF only shuts down this bridge/dashboard, never the sync worker.
        bridge?.terminate()
        if lockFD >= 0 { flock(lockFD, LOCK_UN); close(lockFD) }
    }
    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool { false }
}
let app = NSApplication.shared
let delegate = AppDelegate()
app.delegate = delegate
app.setActivationPolicy(.accessory)
app.run()
