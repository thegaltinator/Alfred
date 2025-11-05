import Cocoa
import SwiftUI

@main
class AppDelegate: NSObject, NSApplicationDelegate {

    var statusBarItem: NSStatusItem!
    var popover: NSPopover!

    func applicationDidFinishLaunching(_ aNotification: Notification) {
        // Create the status bar item
        statusBarItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.squareLength)

        if let button = statusBarItem.button {
            button.image = NSImage(systemSymbolName: "brain.head.profile", accessibilityDescription: "Alfred")
            button.action = #selector(togglePopover(_:))
        }

        // Create the popover
        popover = NSPopover()
        popover.contentSize = NSSize(width: 300, height: 400)
        popover.contentViewController = NSHostingController(rootView: ContentView())
        popover.behavior = .transient
    }

    @objc func togglePopover(_ sender: AnyObject?) {
        if let button = statusBarItem.button {
            if popover.isShown {
                popover.performClose(sender)
            } else {
                popover.show(relativeTo: button.bounds, of: button, preferredEdge: .minY)
            }
        }
    }

    func applicationWillTerminate(_ aNotification: Notification) {
        // Insert code here to tear down your application
    }

    func applicationSupportsSecureRestorableState(_ app: NSApplication) -> Bool {
        return true
    }
}