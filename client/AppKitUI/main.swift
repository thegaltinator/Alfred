import Cocoa
import SwiftUI
import Bridge
import Heartbeat

let app = NSApplication.shared
let delegate = StatusBar()
app.delegate = delegate
app.run()

class StatusBar: NSObject, NSApplicationDelegate {
    var statusBarItem: NSStatusItem!
    var popover: NSPopover!
    private var heartbeatClient: Heartbeat.HeartbeatClient?

    func applicationDidFinishLaunching(_ aNotification: Notification) {
        print("✅ Alfred menubar app starting")

        let environment = Bridge.AlfredEnvironment.shared
        heartbeatClient = Heartbeat.HeartbeatClient(baseURL: environment.cloudBaseURL)
        heartbeatClient?.start()

        // Create the status bar item
        statusBarItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.squareLength)

        if let button = statusBarItem.button {
            button.image = NSImage(systemSymbolName: "brain.head.profile", accessibilityDescription: "Alfred")
            button.action = #selector(showMenu(_:))
            button.sendAction(on: [.leftMouseUp, .rightMouseUp])
        }

        print("🚀 Alfred menubar app launched!")
        print("📍 Heartbeat running - check Redis stream for data")
    }

    @objc func showMenu(_ sender: AnyObject?) {
        let menu = NSMenu()

        // Test Heartbeat item
        let testHeartbeatItem = NSMenuItem(title: "Test Heartbeat", action: #selector(testHeartbeat(_:)), keyEquivalent: "")
        testHeartbeatItem.target = self
        menu.addItem(testHeartbeatItem)

        menu.addItem(NSMenuItem.separator())

        // Quit item
        let quitItem = NSMenuItem(title: "Quit", action: #selector(quitApp(_:)), keyEquivalent: "q")
        quitItem.target = self
        menu.addItem(quitItem)

        // Show the menu
        statusBarItem.menu = menu
        statusBarItem.button?.performClick(nil)

        // Clean up after menu is closed
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.1) {
            self.statusBarItem.menu = nil
        }
    }

    @objc func testHeartbeat(_ sender: AnyObject?) {
        print("🧪 Manual heartbeat test triggered")
        // The heartbeat runs automatically every 5 seconds
    }

    @objc func quitApp(_ sender: AnyObject?) {
        heartbeatClient?.stop()
        NSApplication.shared.terminate(nil)
    }

    func applicationWillTerminate(_ aNotification: Notification) {
        heartbeatClient?.stop()
        print("👋 Alfred menubar app terminated")
    }
}