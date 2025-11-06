import Foundation
import SwiftUI
import Memory

/// TalkerBridge - single pipe for Talker integration per architecture
/// Provides the only integration path the Talker needs:
/// - Cerberas 120B (prompt/stream) [placeholder for C-04]
/// - Read whiteboard (Server-Sent Events or WebSocket) [placeholder for C-04]
/// - Call Planner tool (endpoint) [placeholder for C-04]
/// - Write Memory locally (triggers MemorySync) [IMPLEMENTED for C-04]
/// Talker never writes to the whiteboard
class TalkerBridge: ObservableObject {

    // MARK: - Properties

    /// Memory store instance
    private let memoryStore: SQLiteStore

    /// Embedding runner for Qwen3-Embedding-0.6B
    private let embedRunner: EmbedRunner?

    /// Cached initialization error for embeddings
    private var embedInitializationError: Error?

    /// Published properties for UI updates
    @Published var notesCount: Int = 0
    @Published var noteInput: String = ""
    @Published var alertMessage: String = ""
    @Published var showingAlert: Bool = false
    @Published var recentNotes: [Note] = []
    @Published var embeddingsReady: Bool = false
    @Published var embeddingStatus: String = "Initializing embeddings…"

    // MARK: - Initialization

    init() {
        do {
            self.memoryStore = try SQLiteStore()
            print("✅ TalkerBridge: Memory store initialized successfully")
        } catch {
            fatalError("❌ TalkerBridge: Failed to initialize memory store: \(error.localizedDescription)")
        }

        do {
            let runner = try EmbedRunner()
            self.embedRunner = runner
            DispatchQueue.main.async {
                self.embeddingsReady = runner.ready
                self.embeddingStatus = "Embeddings ready (Qwen3-Embedding-0.6B)"
            }
        } catch {
            self.embedRunner = nil
            self.embedInitializationError = error
            DispatchQueue.main.async { [weak self] in
                guard let self else { return }
                self.embeddingsReady = false
                self.embeddingStatus = "Embeddings unavailable: \(error.localizedDescription)"
                self.showAlert("Embedding initialization failed: \(error.localizedDescription)")
            }
            print("❌ TalkerBridge: Failed to initialize EmbedRunner: \(error.localizedDescription)")
        }

        DispatchQueue.main.async { [weak self] in
            self?.refreshNotesCount()
            self?.loadRecentNotes()
        }
    }

    // MARK: - Actions

    /// Add a new note to the memory store
    func addNote() {
        guard !noteInput.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else {
            showAlert("Please enter some content for the note")
            return
        }

        guard let embedRunner, embedRunner.ready else {
            if let initializationError = embedInitializationError {
                showAlert("Embeddings unavailable: \(initializationError.localizedDescription)")
            } else {
                showAlert("Embeddings are still loading. Please try again shortly.")
            }
            return
        }

        let content = noteInput.trimmingCharacters(in: .whitespacesAndNewlines)
        let runner = embedRunner

        Task(priority: .userInitiated) { [weak self] in
            guard let self else { return }
            do {
                let embedding = try await runner.embed(content)
                let note = try self.memoryStore.addNote(content: content)
                do {
                    try self.memoryStore.storeEmbedding(noteId: note.id, embedding: embedding)
                } catch {
                    try? self.memoryStore.deleteNote(uuid: note.uuid)
                    throw error
                }

                await MainActor.run {
                    self.noteInput = ""
                    self.refreshNotesCount()
                    self.loadRecentNotes()
                    self.showAlert("Note added successfully!")
                }
            } catch {
                await MainActor.run {
                    self.showAlert("Failed to add note: \(error.localizedDescription)")
                }
            }
        }
    }

    /// Refresh the notes count from the memory store
    func refreshNotesCount() {
        do {
            notesCount = try memoryStore.getNotesCount()
            print("📊 TalkerBridge: Notes count refreshed: \(notesCount)")
        } catch {
            print("❌ TalkerBridge: Failed to get notes count: \(error.localizedDescription)")
            showAlert("Failed to refresh notes: \(error.localizedDescription)")
        }
    }

    /// Load recent notes from the memory store
    func loadRecentNotes() {
        do {
            recentNotes = try memoryStore.getRecentNotes(limit: 10)
        } catch {
            print("❌ TalkerBridge: Failed to load recent notes: \(error.localizedDescription)")
        }
    }

    /// Show an alert with the given message
    private func showAlert(_ message: String) {
        alertMessage = message
        showingAlert = true
    }
}

/// Simple SwiftUI view for testing memory operations
/// Can be used in a popover or window for development/testing
struct MemoryTestView: View {
    @StateObject private var bridge = TalkerBridge()

    var body: some View {
        VStack(spacing: 20) {
            Text("🤖 Alfred Memory Test")
                .font(.title)
                .fontWeight(.bold)

            VStack(alignment: .leading, spacing: 8) {
                Text("Total Notes: \(bridge.notesCount)")
                    .font(.headline)

                Text("Tests C-04/C-05: Memory + Embeddings")
                    .font(.caption)
                    .foregroundColor(.secondary)
            }

            VStack(alignment: .leading, spacing: 6) {
                Text("Embedding Status")
                    .font(.headline)
                Text(bridge.embeddingStatus)
                    .font(.caption)
                    .foregroundColor(bridge.embeddingsReady ? .secondary : Color(NSColor.systemRed))
            }

            VStack(alignment: .leading, spacing: 8) {
                Text("Add Test Note")
                    .font(.headline)

                TextEditor(text: $bridge.noteInput)
                    .frame(minHeight: 60)
                    .padding(4)
                    .background(Color(NSColor.controlBackgroundColor))
                    .cornerRadius(6)

                HStack {
                    Button("Add Note") {
                        bridge.addNote()
                    }
                    .disabled(
                        bridge.noteInput.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || !bridge.embeddingsReady
                    )

                    Button("Refresh") {
                        bridge.refreshNotesCount()
                        bridge.loadRecentNotes()
                    }

                    Spacer()
                }
            }

            Divider()

            Text("Recent Notes (\(bridge.recentNotes.count))")
                .font(.headline)

            ScrollView {
                LazyVStack(spacing: 4) {
                    ForEach(Array(bridge.recentNotes.enumerated()), id: \.element.uuid) { index, note in
                        VStack(alignment: .leading, spacing: 2) {
                            HStack {
                                Text("\(index + 1).")
                                    .font(.caption)
                                    .foregroundColor(.secondary)
                                Text(note.createdAt)
                                    .font(.caption)
                                    .foregroundColor(.secondary)
                                Spacer()
                            }
                            Text(note.content)
                                .font(.subheadline)
                                .lineLimit(2)
                        }
                        .padding(.vertical, 2)
                        .padding(.horizontal, 4)
                        .background(Color(NSColor.controlBackgroundColor))
                        .cornerRadius(4)
                    }
                }
            }

            Spacer()
        }
        .padding()
        .frame(width: 350, height: 400)
        .alert("Alfred Memory", isPresented: $bridge.showingAlert, actions: {
            Button("OK", role: .cancel) { }
        }, message: {
            Text(bridge.alertMessage)
        })
        .onAppear {
            bridge.loadRecentNotes()
        }
    }
}
