import AppKit
import CoreImage

struct Bootstrap: Decodable { let token: String; let admin: String; let local: String; let urls: [String] }
final class AppDelegate: NSObject, NSApplicationDelegate {
    var process: Process?
    var input: Pipe?
    var statusItem: NSStatusItem?
    var pageURL: URL?
    var sessionDirectory: URL?
    var starting = false
    var quitting = false
    var window: NSWindow?
    var stateLabel: NSTextField?
    func applicationDidFinishLaunching(_ notification: Notification) {
        NSApp.setActivationPolicy(.regular)
        let mainMenu = NSMenu()
        let appMenuItem = NSMenuItem(); let appMenu = NSMenu()
        appMenu.addItem(withTitle:"退出近传",action:#selector(quit),keyEquivalent:"q").target=self
        appMenuItem.submenu=appMenu; mainMenu.addItem(appMenuItem); NSApp.mainMenu=mainMenu
        let panel=NSWindow(contentRect:NSRect(x:0,y:0,width:460,height:250),styleMask:[.titled,.closable,.miniaturizable],backing:.buffered,defer:false)
        panel.title="近传"; panel.isReleasedWhenClosed=false; panel.center()
        let heading=NSTextField(labelWithString:"↗ 近传 · 你的局域网收发站")
        heading.font=NSFont.systemFont(ofSize:21,weight:.semibold)
        let state=NSTextField(wrappingLabelWithString:"正在启动传输服务…")
        state.font=NSFont.systemFont(ofSize:13); state.textColor = .secondaryLabelColor; stateLabel=state
        let open=NSButton(title:"打开收发界面",target:self,action:#selector(openPage));open.bezelStyle = .rounded
        let folder=NSButton(title:"接收文件目录",target:self,action:#selector(openStorage));folder.bezelStyle = .rounded
        let quitButton=NSButton(title:"退出近传",target:self,action:#selector(quit));quitButton.bezelStyle = .rounded
        let buttons=NSStackView(views:[open,folder,quitButton]);buttons.orientation = .horizontal;buttons.spacing=8
        let stack=NSStackView(views:[heading,state,buttons]);stack.orientation = .vertical;stack.alignment = .leading;stack.spacing=22;stack.translatesAutoresizingMaskIntoConstraints=false
        panel.contentView!.addSubview(stack)
        NSLayoutConstraint.activate([stack.leadingAnchor.constraint(equalTo:panel.contentView!.leadingAnchor,constant:28),stack.trailingAnchor.constraint(equalTo:panel.contentView!.trailingAnchor,constant:-28),stack.centerYAnchor.constraint(equalTo:panel.contentView!.centerYAnchor)])
        window=panel;panel.makeKeyAndOrderFront(nil)
        let item = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
        item.button?.title = "↗ 近传"
        let menu = NSMenu()
        menu.addItem(withTitle: "打开近传", action: #selector(openPage), keyEquivalent: "o").target = self
        menu.addItem(withTitle: "打开接收文件目录", action: #selector(openStorage), keyEquivalent: "").target = self
        menu.addItem(NSMenuItem.separator())
        menu.addItem(withTitle: "退出近传", action: #selector(quit), keyEquivalent: "q").target = self
        item.menu = menu; statusItem = item
        start()
    }
    var storage: URL {
        // An explicit isolated directory is useful for automated acceptance without touching user data.
        if let path = ProcessInfo.processInfo.environment["NEARTRANSFER_DATA_DIR"] { return URL(fileURLWithPath:path, isDirectory:true) }
        return FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0].appendingPathComponent("NearTransfer/Inbox",isDirectory:true)
    }
    func start() {
        guard !starting else { return }; starting = true
        do {
            try FileManager.default.createDirectory(at: storage.deletingLastPathComponent(), withIntermediateDirectories: true)
            let temp = FileManager.default.temporaryDirectory.appendingPathComponent("NearTransfer-\(UUID().uuidString)",isDirectory:true)
            try FileManager.default.createDirectory(at: temp, withIntermediateDirectories: false, attributes: [.posixPermissions:0o700]); sessionDirectory = temp
            guard let engine = Bundle.main.url(forResource:"neartransfer-engine", withExtension:nil) else { throw NSError(domain:"NearTransfer",code:1,userInfo:[NSLocalizedDescriptionKey:"应用内缺少传输引擎，请重新构建或安装。"]) }
            let child = Process();child.executableURL=engine;child.arguments=[storage.path,temp.path]
            let output=Pipe(), errors=Pipe(), stdin=Pipe();child.standardOutput=output;child.standardError=errors;child.standardInput=stdin;input=stdin;process=child
            child.terminationHandler = { [weak self] _ in
                let data = errors.fileHandleForReading.readDataToEndOfFile()
                DispatchQueue.main.async {
                    guard let self=self, !self.quitting else{return}
                    self.pageURL=nil;self.starting=false;self.stateLabel?.stringValue="服务已停止，请退出并重新打开。"
                    self.showError(String(data:data,encoding:.utf8) ?? "传输服务已停止，请退出并重新打开近传。")
                }
            }
            try child.run()
            // Read a bounded single bootstrap line off the main thread, keeping the menu responsive.
            DispatchQueue.global().async { [weak self] in
                do {
                    var data=Data()
                    while data.count < 65536 {
                        guard let byte=try output.fileHandleForReading.read(upToCount:1), !byte.isEmpty else {throw NSError(domain:"NearTransfer",code:2,userInfo:[NSLocalizedDescriptionKey:"传输服务未能启动。"])}
                        if byte == Data([10]) {break}; data.append(byte)
                    }
                    let bootstrap=try JSONDecoder().decode(Bootstrap.self,from:data)
                    for (index,base) in bootstrap.urls.enumerated() {
                        guard let filter=CIFilter(name:"CIQRCodeGenerator") else {throw NSError(domain:"NearTransfer",code:3)}
                        filter.setValue(Data("\(base)/#token=\(bootstrap.token)".utf8),forKey:"inputMessage")
                        filter.setValue("M",forKey:"inputCorrectionLevel")
                        guard let ci=filter.outputImage,let cg=CIContext().createCGImage(ci.transformed(by:CGAffineTransform(scaleX:8,y:8)),from:ci.extent.applying(CGAffineTransform(scaleX:8,y:8))) else {throw NSError(domain:"NearTransfer",code:4)}
                        let rep=NSBitmapImageRep(cgImage:cg)
                        guard let png=rep.representation(using:.png,properties:[:]) else {throw NSError(domain:"NearTransfer",code:5)}
                        try png.write(to:temp.appendingPathComponent("\(index).png"),options:.atomic)
                    }
                    let url=URL(string:"\(bootstrap.local)/#token=\(bootstrap.token)&admin=\(bootstrap.admin)")!
                    DispatchQueue.main.async {guard let self=self,!self.quitting else{return};self.starting=false;self.pageURL=url;self.stateLabel?.stringValue="正在运行。手机与 Mac 连接同一网络，在收发界面扫码连接。关闭浏览器不会停止传输；退出近传会断开连接。";self.openPage()}
                } catch {DispatchQueue.main.async {guard let self=self,!self.quitting else{return};self.starting=false;self.showError(error.localizedDescription)}}
            }
            DispatchQueue.main.asyncAfter(deadline:.now()+15) { [weak self,weak child] in
                guard let self=self,self.starting,let child=child,child.isRunning else{return}
                child.terminate();self.starting=false
            }
        } catch {starting=false;showError(error.localizedDescription)}
    }
    @objc func openPage() { if let url=pageURL {NSWorkspace.shared.open(url)} else if !starting {showError("服务未运行，请退出并重新打开近传。")}}
    @objc func openStorage() {NSWorkspace.shared.open(storage)}
    @objc func quit() {NSApp.terminate(nil)}
    func showError(_ message:String) {stateLabel?.stringValue=message;let alert=NSAlert();alert.messageText="近传暂时无法连接";alert.informativeText=message;alert.addButton(withTitle:"好");NSApp.activate(ignoringOtherApps:true);alert.runModal()}
    func applicationShouldTerminate(_ sender:NSApplication) -> NSApplication.TerminateReply {
        quitting=true
        try? input?.fileHandleForWriting.close()
        guard let child=process,child.isRunning else {cleanup();return .terminateNow}
        child.terminate()
        DispatchQueue.global().async { [weak self] in
            child.waitUntilExit()
            DispatchQueue.main.async {self?.cleanup();sender.reply(toApplicationShouldTerminate:true)}
        }
        return .terminateLater
    }
    func cleanup() {if let dir=sessionDirectory {try? FileManager.default.removeItem(at:dir)}}
    func applicationShouldHandleReopen(_ sender:NSApplication,hasVisibleWindows flag:Bool)->Bool {window?.makeKeyAndOrderFront(nil);openPage();return true}
}
let app=NSApplication.shared
let delegate=AppDelegate()
app.delegate=delegate
app.run()
