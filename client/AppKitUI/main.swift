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
    private var backendPopover: NSPopover?

    func applicationDidFinishLaunching(_ aNotification: Notification) {
        print("✅ Alfred menubar app starting")

        let environment = Bridge.AlfredEnvironment.shared
        heartbeatClient = Heartbeat.HeartbeatClient(baseURL: environment.cloudBaseURL)
        heartbeatClient?.start()

        Task.detached {
            await TalkerService.shared.warmUp()
        }

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

        let backendItem = NSMenuItem(title: "Test Backend", action: #selector(openBackendTest(_:)), keyEquivalent: "")
        backendItem.target = self
        menu.addItem(backendItem)

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

    @objc private func openBackendTest(_ sender: AnyObject?) {
        if backendPopover == nil {
            let popover = NSPopover()
            popover.behavior = .semitransient
            popover.contentSize = NSSize(width: 360, height: 420)
            backendPopover = popover
        }

        backendPopover?.contentViewController = NSHostingController(rootView: BackendTestView())

        if let button = statusBarItem.button, let pop = backendPopover {
            pop.show(relativeTo: button.bounds, of: button, preferredEdge: .maxY)
            NSApp.activate(ignoringOtherApps: true)
        }
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

struct BackendTestView: View {
    @State private var textInput = ""
    @State private var response = ""
    @State private var isWaiting = false

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            Text("Backend Test")
                .font(.title2)
                .fontWeight(.semibold)

            ScrollView {
                Text(response.isEmpty ? "Ready." : response)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(8)
                    .background(Color.gray.opacity(0.1))
                    .cornerRadius(8)
            }
            .frame(maxHeight: 200)

            VStack {
                TextEditor(text: $textInput)
                    .frame(minHeight: 80)
                    .overlay(
                        RoundedRectangle(cornerRadius: 8)
                            .stroke(Color.gray.opacity(0.3), lineWidth: 1)
                    )

                HStack {
                    Spacer()
                    Button(isWaiting ? "Sending…" : "Send") {
                        sendPrompt()
                    }
                    .disabled(textInput.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || isWaiting)
                }
            }

            Spacer()
        }
        .padding()
        .frame(width: 360, height: 420)
    }

    private func sendPrompt() {
        let prompt = textInput.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !prompt.isEmpty else { return }
        textInput = ""
        response = "Thinking…"
        isWaiting = true

        Task {
            do {
                let final = try await TalkerService.shared.sendMessage(prompt) { partial in
                    Task { @MainActor in
                        self.response = partial
                    }
                }
                await MainActor.run {
                    self.response = final
                    self.isWaiting = false
                }
            } catch {
                await MainActor.run {
                    self.response = "❌ \(error.localizedDescription)"
                    self.isWaiting = false
                }
            }
        }
    }
}
