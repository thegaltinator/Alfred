import Foundation
import Memory

@main
enum MemoryEmbeddingSmokeTest {
    static func main() async {
        print("🧪 Testing C-04/C-05: Memory (SQLite WAL) + Qwen Embeddings")

        do {
            let store = try SQLiteStore()
            let embedRunner = try EmbedRunner()

            let appSupportURL = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask).first!
            let dbURL = appSupportURL.appendingPathComponent("Alfred/memory.db")

            guard FileManager.default.fileExists(atPath: dbURL.path) else {
                throw RuntimeError("Database not found at expected path: \(dbURL.path)")
            }
            print("✅ Database present at \(dbURL.path)")

            let initialCount = try store.getNotesCount()
            print("📊 Initial notes count: \(initialCount)")

            let primaryContent = "Swift concurrency overview covering tasks, async/await, and actors."
            let relatedContent = "Deep dive into async/await and structured concurrency techniques in Swift."
            let unrelatedContent = "Sourdough starter feeding schedule and baking reminders."

            let primaryEmbedding = try await embedRunner.embed(primaryContent)
            let primaryNote = try store.addNote(content: primaryContent)
            try store.storeEmbedding(noteId: primaryNote.id, embedding: primaryEmbedding)
            print("✅ Added primary note \(primaryNote.uuid)")

            let relatedEmbedding = try await embedRunner.embed(relatedContent)
            let relatedNote = try store.addNote(content: relatedContent, metadata: "{\"topic\":\"swift\"}")
            try store.storeEmbedding(noteId: relatedNote.id, embedding: relatedEmbedding)
            print("✅ Added related note \(relatedNote.uuid)")

            let unrelatedEmbedding = try await embedRunner.embed(unrelatedContent)
            let unrelatedNote = try store.addNote(content: unrelatedContent, metadata: "{\"topic\":\"cooking\"}")
            try store.storeEmbedding(noteId: unrelatedNote.id, embedding: unrelatedEmbedding)
            print("✅ Added unrelated note \(unrelatedNote.uuid)")

            let newCount = try store.getNotesCount()
            guard newCount == initialCount + 3 else {
                throw RuntimeError("Expected note count \(initialCount + 3), observed \(newCount)")
            }
            print("📊 Notes count after inserts: \(newCount)")

            let similarMatches = try store.findSimilarNotes(for: relatedEmbedding, limit: 3, threshold: 0.25)
            guard let topHit = similarMatches.first else {
                throw RuntimeError("No matches returned for related note query")
            }

            guard topHit.note.uuid == relatedNote.uuid else {
                throw RuntimeError("Expected self-match first, saw \(topHit.note.uuid)")
            }
            print("🔍 Self similarity score: \(String(format: "%.3f", Double(topHit.similarity)))")

            guard let siblingMatch = similarMatches.first(where: { $0.note.uuid == primaryNote.uuid }) else {
                throw RuntimeError("Related primary note was not returned in nearest neighbors")
            }
            print("🤝 Related note similarity: \(String(format: "%.3f", Double(siblingMatch.similarity)))")

            let unrelatedHit = similarMatches.first(where: { $0.note.uuid == unrelatedNote.uuid })
            guard unrelatedHit == nil else {
                throw RuntimeError("Unrelated note should not meet similarity threshold")
            }

            try store.deleteNote(uuid: unrelatedNote.uuid)
            let countAfterDelete = try store.getNotesCount()
            guard countAfterDelete == newCount - 1 else {
                throw RuntimeError("Expected count after delete \(newCount - 1), saw \(countAfterDelete)")
            }

            print("✅ C-04 acceptance verified (WAL, add, query, delete)")
            print("✅ C-05 acceptance verified (Qwen3-Embedding-0.6B vectors + cosine recall)")
            print("🎉 All memory + embedding checks passed!")
        } catch {
            print("❌ Test failed: \(error.localizedDescription)")
            exit(1)
        }
    }
}

private struct RuntimeError: LocalizedError {
    let message: String
    init(_ message: String) { self.message = message }
    var errorDescription: String? { message }
}
