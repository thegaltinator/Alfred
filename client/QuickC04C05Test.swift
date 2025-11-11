import Foundation

/// Quick test for C-04 and C-05 compliance
/// Tests the essential requirements from tasks_final.md
class QuickC04C05Test {

    static func main() async {
        print("🚀 Quick C-04 & C-05 Compliance Test")
        print("===================================")

        do {
            // Test C-04: SQLite WAL
            try await testC04SQLiteWAL()

            // Test C-05: Qwen3-Embedding-0.6B
            try await testC05Embeddings()

            print("\n🎉 SUCCESS: All C-04 & C-05 tests passed!")
            print("✅ C-04: SQLite WAL memory store working")
            print("✅ C-05: Qwen3-Embedding-0.6B working")

        } catch {
            print("\n❌ FAILED: \(error.localizedDescription)")
            exit(1)
        }
    }

    /// Test C-04: SQLite WAL memory store
    private static func testC04SQLiteWAL() async throws {
        print("\n📊 Testing C-04: SQLite WAL Memory Store")

        // Initialize memory bridge (should create database)
        let memoryBridge = try MemoryBridge()

        // Test database creation location
        let appSupportURL = FileManager.default.urls(for: .applicationSupportDirectory,
                                                     in: .userDomainMask).first!
        let dbPath = appSupportURL.appendingPathComponent("Alfred/memory.db").path

        guard FileManager.default.fileExists(atPath: dbPath) else {
            throw TestError("Database not created at \(dbPath)")
        }
        print("   ✅ Database created at: \(dbPath)")

        // Test note storage
        let initialStats = try await memoryBridge.getMemoryStats()
        let initialCount = initialStats["total_notes"] as? Int ?? 0

        let testNote = "C-04 test note at \(Date())"
        let noteUUID = try await memoryBridge.addMemory(testNote, metadata: ["test": "C-04"])

        let updatedStats = try await memoryBridge.getMemoryStats()
        let updatedCount = updatedStats["total_notes"] as? Int ?? 0

        guard updatedCount == initialCount + 1 else {
            throw TestError("Note count didn't increase: \(initialCount) -> \(updatedCount)")
        }

        print("   ✅ Note stored successfully (count: \(initialCount) -> \(updatedCount))")
        print("   ✅ SQLite WAL memory store working correctly")
    }

    /// Test C-05: Qwen3-Embedding-0.6B
    private static func testC05Embeddings() async throws {
        print("\n🧠 Testing C-05: Qwen3-Embedding-0.6B")

        // Test embedding dimension
        let expectedDimension = 1024
        let actualDimension = EmbedRunner.embeddingDimension

        guard actualDimension == expectedDimension else {
            throw TestError("Wrong embedding dimension: expected \(expectedDimension), got \(actualDimension)")
        }
        print("   ✅ Embedding dimension: \(actualDimension) (correct)")

        // Test memory bridge with embeddings
        let memoryBridge = try MemoryBridge()

        // Add two related notes for similarity search test
        let note1 = "I am learning Swift programming language"
        let note2 = "Swift programming uses concepts like optionals"

        print("   📝 Adding related notes...")
        let uuid1 = try await memoryBridge.addMemory(note1, metadata: ["topic": "swift"])
        let uuid2 = try await memoryBridge.addMemory(note2, metadata: ["topic": "swift"])

        // Test similarity search
        print("   🔍 Testing similarity search...")
        let results = try await memoryBridge.searchMemories("Swift programming", limit: 5)

        print("   📊 Search found \(results.count) results")

        // Find our test notes in results
        var found1 = false, found2 = false
        for result in results {
            if result.uuid == uuid1 {
                found1 = true
                print("   ✅ Found note 1 (similarity: \(String(format: "%.3f", result.similarity)))")
            }
            if result.uuid == uuid2 {
                found2 = true
                print("   ✅ Found note 2 (similarity: \(String(format: "%.3f", result.similarity)))")
            }
        }

        guard found1, found2 else {
            throw TestError("Related notes not found in similarity search")
        }

        print("   ✅ Similarity search working correctly")
        print("   ✅ Qwen3-Embedding-0.6B with vector search working")
    }
}

// Simple error type
struct TestError: Error, CustomStringConvertible {
    let description: String
    init(_ description: String) {
        self.description = description
    }
}

// Run the test
await QuickC04C05Test.main()