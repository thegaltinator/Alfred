import Cocoa
import SwiftUI
import Bridge
import Heartbeat
import Memory

let app = NSApplication.shared
let delegate = StatusBar()
app.delegate = delegate
app.run()

class StatusBar: NSObject, NSApplicationDelegate {
    var statusBarItem: NSStatusItem!
    var popover: NSPopover!
    private var heartbeatClient: Heartbeat.HeartbeatClient?
    private var backendPopover: NSPopover?
    private var memoryPopover: NSPopover?
    private var whiteboardPopover: NSPopover?

    func applicationDidFinishLaunching(_ aNotification: Notification) {
        print("✅ Alfred menubar app starting")

        // Ignore SIGPIPE to prevent crashes when writing to closed pipes
        signal(SIGPIPE, SIG_IGN)

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

        let newThreadItem = NSMenuItem(title: "New Conversation", action: #selector(startNewConversation(_:)), keyEquivalent: "")
        newThreadItem.target = self
        menu.addItem(newThreadItem)

        let addNoteItem = NSMenuItem(title: "Add Note…", action: #selector(openAddNote(_:)), keyEquivalent: "")
        addNoteItem.target = self
        menu.addItem(addNoteItem)

        let whiteboardItem = NSMenuItem(title: "Whiteboard", action: #selector(openWhiteboard(_:)), keyEquivalent: "")
        whiteboardItem.target = self
        menu.addItem(whiteboardItem)

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

    @objc func startNewConversation(_ sender: AnyObject?) {
        print("🧵 Starting new conversation thread")
        Task {
            await TalkerService.shared.startNewThread()
        }
    }

    @objc private func openBackendTest(_ sender: AnyObject?) {
        if backendPopover == nil {
            let popover = NSPopover()
            popover.behavior = .semitransient
            popover.contentSize = NSSize(width: 360, height: 420)
            backendPopover = popover
        }

        backendPopover?.contentViewController = NSHostingController(rootView: BackendTestView(onClose: {
            self.backendPopover?.performClose(nil)
        }))

        if let button = statusBarItem.button, let pop = backendPopover {
            pop.show(relativeTo: button.bounds, of: button, preferredEdge: .maxY)
            NSApp.activate(ignoringOtherApps: true)
        }
    }

    @objc private func openAddNote(_ sender: AnyObject?) {
        if memoryPopover == nil {
            let popover = NSPopover()
            popover.behavior = .semitransient
            popover.contentSize = NSSize(width: 320, height: 240)
            memoryPopover = popover
        }

        memoryPopover?.contentViewController = NSHostingController(rootView: AddNoteView())

        if let button = statusBarItem.button, let pop = memoryPopover {
            pop.show(relativeTo: button.bounds, of: button, preferredEdge: .maxY)
            NSApp.activate(ignoringOtherApps: true)
        }
    }

    @objc private func openWhiteboard(_ sender: AnyObject?) {
        print("📝 openWhiteboard() called")

        if whiteboardPopover == nil {
            let popover = NSPopover()
            popover.behavior = .semitransient
            popover.contentSize = NSSize(width: 400, height: 500)
            whiteboardPopover = popover
        }

        print("📝 Creating WhiteboardView")
        whiteboardPopover?.contentViewController = NSHostingController(rootView: WhiteboardView(onClose: {
            print("📝 Whiteboard onClose called")
            self.whiteboardPopover?.performClose(nil)
        }))

        if let button = statusBarItem.button, let pop = whiteboardPopover {
            print("📝 Showing whiteboard popover")
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
    let onClose: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            HStack {
                Text("Backend Test")
                    .font(.title2)
                    .fontWeight(.semibold)
                Spacer()
                Button(action: {
                    closeBackendTest()
                }) {
                    Image(systemName: "xmark.circle.fill")
                        .font(.title2)
                        .foregroundColor(.secondary)
                }
                .buttonStyle(BorderlessButtonStyle())
                .help("Close")
            }

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

            Divider()

            MemoryNoteForm(title: "Memory (SQLite)")

            Spacer()
        }
        .padding()
        .frame(width: 360, height: 420)
    }

    private func sendPrompt() {
        print("🎯 UI: sendPrompt called")
        let prompt = textInput.trimmingCharacters(in: .whitespacesAndNewlines)
        print("🎯 UI: prompt = '\(prompt)'")
        guard !prompt.isEmpty else {
            print("🎯 UI: empty prompt, returning")
            return
        }
        textInput = ""
        response = "Thinking…"
        isWaiting = true
        print("🎯 UI: starting Task...")

        Task {
            do {
                print("🎯 UI: calling TalkerService.shared.sendMessage")
                let final = try await TalkerService.shared.sendMessage(prompt) { partial in
                    print("🎯 UI: got partial response: '\(String(partial.prefix(100)))'")
                    Task { @MainActor in
                        self.response = partial
                    }
                }
                print("🎯 UI: got final response: '\(String(final.prefix(100)))'")
                await MainActor.run {
                    self.response = final
                    self.isWaiting = false
                }
                print("🎯 UI: success, updated UI")
            } catch {
                print("🎯 UI: ERROR: \(error)")
                print("🎯 UI: ERROR type: \(type(of: error))")
                print("🎯 UI: ERROR localized: \(error.localizedDescription)")
                await MainActor.run {
                    self.response = "❌ \(error.localizedDescription)"
                    self.isWaiting = false
                }
                print("🎯 UI: error handling complete")
            }
        }
        print("🎯 UI: sendPrompt function ended")
    }

    private func closeBackendTest() {
        onClose()
    }
}

struct AddNoteView: View {
    var body: some View {
        MemoryNoteForm(title: "Add Note")
            .padding()
            .frame(width: 320, height: 220)
    }
}

struct MemoryNoteForm: View {
    let title: String
    @State private var noteText = ""
    @State private var noteStatus = "No notes saved yet."

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(title)
                .font(.headline)

            TextEditor(text: $noteText)
                .frame(minHeight: 60, maxHeight: 80)
                .overlay(
                    RoundedRectangle(cornerRadius: 8)
                        .stroke(Color.gray.opacity(0.3), lineWidth: 1)
                )

            HStack {
                Spacer()
                Button("Add Note") {
                    addNote()
                }
                .disabled(noteText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
            }

            Text(noteStatus)
                .font(.caption)
                .foregroundStyle(noteStatus.hasPrefix("❌") ? Color.red : Color.secondary)
        }
    }

    private func addNote() {
        let trimmed = noteText.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else {
            noteStatus = "Enter a note before saving."
            return
        }

        noteStatus = "Saving…"
        Task {
            do {
                let noteID = try SQLiteStore.shared.addNote(content: trimmed)
                let total = try SQLiteStore.shared.noteCount()
                await MainActor.run {
                    self.noteText = ""
                    self.noteStatus = "✅ Saved note #\(noteID). Total notes: \(total)."
                }
            } catch {
                await MainActor.run {
                    self.noteStatus = "❌ \(error.localizedDescription)"
                }
            }
        }
    }
}
