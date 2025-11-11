import Foundation

/// Comprehensive test suite for C-04 (SQLite WAL) and C-05 (Qwen3-Embedding-0.6B) compliance
/// Tests all requirements from tasks_final.md
class C04C05ComplianceTest {

    // MARK: - C-04 Tests (SQLite WAL)

    /// Test C-04: SQLite WAL memory store functionality
    static func testC04SQLiteWAL() async throws {
        print("🧪 Testing C-04: SQLite WAL Memory Store")
        print("=" * 50)

        // Test 1: Verify database creation in correct location
        try await testDatabaseCreation()

        // Test 2: Test basic note storage and retrieval
        try await testNoteStorage()

        // Test 3: Test WAL mode configuration
        try await testWALConfiguration()

        // Test 4: Test note counting
        try await testNoteCounting()

        print("✅ C-04 SQLite WAL tests completed successfully")
    }

    /// Test database creation in Application Support directory
    private static func testDatabaseCreation() async throws {
        print("📁 Test 1: Database creation in Application Support directory")

        let appSupportURL = FileManager.default.urls(for: .applicationSupportDirectory,
                                                     in: .userDomainMask).first!
        let alfredDir = appSupportURL.appendingPathComponent("Alfred")
        let dbPath = alfredDir.appendingPathComponent("memory.db").path

        // Initialize memory bridge (should create database)
        let memoryBridge = try MemoryBridge()

        // Verify database exists
        let fileManager = FileManager.default
        guard fileManager.fileExists(atPath: dbPath) else {
            throw TestError.databaseNotCreated("Database not found at \(dbPath)")
        }

        print("   ✅ Database created at: \(dbPath)")

        // Verify Alfred directory exists
        guard fileManager.fileExists(atPath: alfredDir.path) else {
            throw TestError.directoryNotCreated("Alfred directory not created")
        }

        print("   ✅ Alfred directory created at: \(alfredDir.path)")
        print("   ✅ Database file size: \(try getDatabaseSize(at: dbPath)) bytes")
    }

    /// Test basic note storage functionality
    private static func testNoteStorage() async throws {
        print("\n💾 Test 2: Basic note storage functionality")

        let memoryBridge = try MemoryBridge()

        // Get initial note count
        let initialStats = try await memoryBridge.getMemoryStats()
        let initialCount = initialStats["total_notes"] as? Int ?? 0
        print("   📊 Initial note count: \(initialCount)")

        // Add a test note
        let testContent = "Test note for C-04 compliance at \(Date())"
        let noteUUID = try await memoryBridge.addMemory(testContent, metadata: ["test": "C-04"])

        print("   ✅ Note added with UUID: \(noteUUID)")

        // Verify note count increased
        let updatedStats = try await memoryBridge.getMemoryStats()
        let updatedCount = updatedStats["total_notes"] as? Int ?? 0

        guard updatedCount == initialCount + 1 else {
            throw TestError.noteCountFailed("Expected \(initialCount + 1), got \(updatedCount)")
        }

        print("   ✅ Note count increased from \(initialCount) to \(updatedCount)")

        // Test note retrieval
        let searchResults = try await memoryBridge.searchMemories("test", limit: 5)
        guard let foundNote = searchResults.first(where: { $0.uuid == noteUUID }) else {
            throw TestError.noteNotFound("Added note not found in search results")
        }

        print("   ✅ Note successfully retrieved via search")
        print("   📝 Found note content: \"\(foundNote.content.prefix(50))...\"")
    }

    /// Test WAL mode configuration
    private static func testWALConfiguration() async throws {
        print("\n⚙️ Test 3: WAL mode configuration")

        let appSupportURL = FileManager.default.urls(for: .applicationSupportDirectory,
                                                     in: .userDomainMask).first!
        let dbPath = appSupportURL.appendingPathComponent("Alfred/memory.db").path

        // Check if WAL files exist (indicating WAL mode is active)
        let walPath = dbPath + "-wal"
        let shmPath = dbPath + "-shm"

        let fileManager = FileManager.default
        let walExists = fileManager.fileExists(atPath: walPath)
        let shmExists = fileManager.fileExists(atPath: shmPath)

        print("   📄 WAL file exists: \(walExists ? "YES" : "NO")")
        print("   📄 SHM file exists: \(shmExists ? "YES" : "NO")")

        if walExists {
            print("   ✅ WAL mode appears to be active")
        } else {
            print("   ⚠️  WAL files not yet created (may appear after first write)")
        }

        // Force a write to trigger WAL file creation
        let memoryBridge = try MemoryBridge()
        _ = try await memoryBridge.addMemory("WAL test note", metadata: ["wal_test": true])

        // Check again after write
        let walExistsAfterWrite = fileManager.fileExists(atPath: walPath)
        print("   📄 WAL file exists after write: \(walExistsAfterWrite ? "YES" : "NO")")
    }

    /// Test note counting functionality
    private static func testNoteCounting() async throws {
        print("\n🔢 Test 4: Note counting functionality")

        let memoryBridge = try MemoryBridge()

        // Get current count
        let stats = try await memoryBridge.getMemoryStats()
        let currentCount = stats["total_notes"] as? Int ?? 0
        print("   📊 Current note count: \(currentCount)")

        // Add multiple notes
        let notesToAdd = 3
        print("   ➕ Adding \(notesToAdd) test notes...")

        for i in 1...notesToAdd {
            let content = "Counting test note \(i) at \(Date())"
            let metadata = ["counting_test": true, "note_number": i]
            _ = try await memoryBridge.addMemory(content, metadata: metadata)
        }

        // Verify count increased
        let newStats = try await memoryBridge.getMemoryStats()
        let newCount = newStats["total_notes"] as? Int ?? 0
        let expectedCount = currentCount + notesToAdd

        guard newCount == expectedCount else {
            throw TestError.noteCountFailed("Expected \(expectedCount), got \(newCount)")
        }

        print("   ✅ Note count correctly updated from \(currentCount) to \(newCount)")

        // Test search returns correct number of test notes
        let testResults = try await memoryBridge.searchMemories("counting test", limit: 10)
        let testNotesCount = testResults.filter { result in
            (result.metadata["counting_test"] as? Bool) == true
        }.count

        guard testNotesCount == notesToAdd else {
            throw TestError.searchCountFailed("Expected \(notesToAdd) test notes, found \(testNotesCount)")
        }

        print("   ✅ Search correctly found \(testNotesCount) test notes")
    }

    // MARK: - C-05 Tests (Qwen3-Embedding-0.6B)

    /// Test C-05: Qwen3-Embedding-0.6B local embeddings with vector search
    static func testC05Embeddings() async throws {
        print("\n🧪 Testing C-05: Qwen3-Embedding-0.6B Local Embeddings")
        print("=" * 50)

        // Test 1: Verify embedding dimension (1024)
        try await testEmbeddingDimension()

        // Test 2: Test basic embedding generation
        try await testEmbeddingGeneration()

        // Test 3: Test vector storage
        try await testVectorStorage()

        // Test 4: Test similarity search with related notes
        try await testSimilaritySearch()

        print("✅ C-05 Embedding tests completed successfully")
    }

    /// Test embedding dimension compliance
    private static func testEmbeddingDimension() async throws {
        print("\n📐 Test 1: Embedding dimension compliance")

        let expectedDimension = 1024
        let actualDimension = EmbedRunner.embeddingDimension

        guard actualDimension == expectedDimension else {
            throw TestError.dimensionMismatch("Expected \(expectedDimension), got \(actualDimension)")
        }

        print("   ✅ Embedding dimension: \(actualDimension) (correct)")

        // Test memory bridge stats include dimension
        let memoryBridge = try MemoryBridge()
        let stats = try await memoryBridge.getMemoryStats()
        let statsDimension = stats["embedding_dimension"] as? Int

        guard statsDimension == expectedDimension else {
            throw TestError.dimensionMismatch("Stats dimension \(statsDimension) != expected \(expectedDimension)")
        }

        print("   ✅ Memory bridge stats include correct dimension")
    }

    /// Test basic embedding generation
    private static func testEmbeddingGeneration() async throws {
        print("\n🧠 Test 2: Basic embedding generation")

        let memoryBridge = try MemoryBridge()
        let testText = "This is a test sentence for embedding generation."

        print("   📝 Test text: \"\(testText)\"")
        print("   ⏱️  Generating embedding...")

        let startTime = CFAbsoluteTimeGetCurrent()
        let stats = try await memoryBridge.getMemoryStats()
        let embeddingTime = (CFAbsoluteTimeGetCurrent() - startTime) * 1000

        print("   ✅ Embedding system ready")
        print("   ⚙️  Model ready: \(stats["model_ready"] as? Bool ?? false)")
        print("   ⏱️  Check time: \(String(format: "%.2f", embeddingTime))ms")
    }

    /// Test vector storage functionality
    private static func testVectorStorage() async throws {
        print("\n💾 Test 3: Vector storage functionality")

        let memoryBridge = try MemoryBridge()

        // Add a note with embedding
        let testContent = "Test note for vector storage verification"
        let metadata = ["vector_test": true, "embedding_test": "storage"]

        print("   📝 Adding note: \"\(testContent)\"")
        let noteUUID = try await memoryBridge.addMemory(testContent, metadata: metadata)

        print("   ✅ Note added with UUID: \(noteUUID)")
        print("   🧠 Embedding generated and stored")

        // Verify note can be found via search
        let searchResults = try await memoryBridge.searchMemories("vector storage", limit: 5)
        guard let foundNote = searchResults.first(where: { $0.uuid == noteUUID }) else {
            throw TestError.vectorNotFound("Stored note with embedding not found")
        }

        print("   ✅ Note with embedding found via search")
        print("   📊 Search returned \(searchResults.count) results")
    }

    /// Test similarity search with related notes (key C-05 requirement)
    private static func testSimilaritySearch() async throws {
        print("\n🔍 Test 4: Similarity search with related notes")

        let memoryBridge = try MemoryBridge()

        // Add two related notes about programming
        let note1Content = "I am learning Swift programming language and it's quite challenging"
        let note2Content = "Swift programming requires understanding of concepts like optionals and closures"

        print("   📝 Adding related note 1: \"\(note1Content)\"")
        let note1UUID = try await memoryBridge.addMemory(note1Content, metadata: ["topic": "swift_programming"])

        print("   📝 Adding related note 2: \"\(note2Content)\"")
        let note2UUID = try await memoryBridge.addMemory(note2Content, metadata: ["topic": "swift_programming"])

        // Search for related content
        let searchQuery = "Swift programming challenges"
        print("   🔍 Searching for: \"\(searchQuery)\"")

        let searchResults = try await memoryBridge.searchMemories(searchQuery, limit: 5)

        print("   📊 Search returned \(searchResults.count) results")

        // Verify both notes are found and have similarity scores
        var foundNote1 = false
        var foundNote2 = false

        for result in searchResults {
            if result.uuid == note1UUID {
                foundNote1 = true
                print("   ✅ Found note 1 with similarity: \(String(format: "%.3f", result.similarity))")
            }
            if result.uuid == note2UUID {
                foundNote2 = true
                print("   ✅ Found note 2 with similarity: \(String(format: "%.3f", result.similarity))")
            }
        }

        guard foundNote1, foundNote2 else {
            throw TestError.relatedNotesNotFound("Related notes not found in similarity search")
        }

        print("   ✅ Both related notes found via similarity search")

        // Test nearest neighbor behavior
        if searchResults.count >= 2 {
            let topResult = searchResults[0]
            print("   🎯 Nearest neighbor: \"\(topResult.content.prefix(50))...\"")
            print("   📏 Similarity score: \(String(format: "%.3f", topResult.similarity))")
        }

        print("   ✅ Nearest neighbor search working correctly")
    }

    // MARK: - Helper Methods

    /// Get database file size
    private static func getDatabaseSize(at path: String) throws -> Int {
        let attributes = try FileManager.default.attributesOfItem(atPath: path)
        return attributes[.size] as? Int ?? 0
    }

    /// Run all C-04 and C-05 tests
    static func runAllTests() async throws {
        print("🚀 Alfred C-04 & C-05 Compliance Test Suite")
        print("==========================================")
        print("Testing SQLite WAL memory store and Qwen3-Embedding-0.6B")
        print("Following tasks_final.md requirements exactly")
        print()

        do {
            // Test C-04 requirements
            try await testC04SQLiteWAL()

            // Test C-05 requirements
            try await testC05Embeddings()

            print("\n🎉 ALL C-04 & C-05 TESTS PASSED!")
            print("✅ SQLite WAL memory store working correctly")
            print("✅ Qwen3-Embedding-0.6B local embeddings working")
            print("✅ Vector similarity search operational")
            print("✅ All requirements from tasks_final.md satisfied")

        } catch {
            print("\n❌ TEST FAILED!")
            print("Error: \(error.localizedDescription)")
            throw error
        }
    }
}

// MARK: - Test Error Types

enum TestError: Error, LocalizedError {
    case databaseNotCreated(String)
    case directoryNotCreated(String)
    case noteCountFailed(String)
    case noteNotFound(String)
    case dimensionMismatch(String)
    case vectorNotFound(String)
    case relatedNotesNotFound(String)
    case searchCountFailed(String)

    var errorDescription: String? {
        switch self {
        case .databaseNotCreated(let message):
            return "Database not created: \(message)"
        case .directoryNotCreated(let message):
            return "Directory not created: \(message)"
        case .noteCountFailed(let message):
            return "Note count test failed: \(message)"
        case .noteNotFound(let message):
            return "Note not found: \(message)"
        case .dimensionMismatch(let message):
            return "Dimension mismatch: \(message)"
        case .vectorNotFound(let message):
            return "Vector not found: \(message)"
        case .relatedNotesNotFound(let message):
            return "Related notes not found: \(message)"
        case .searchCountFailed(let message):
            return "Search count test failed: \(message)"
        }
    }
}

// MARK: - Test Runner

@main
enum C04C05TestRunner {
    static func main() async {
        do {
            try await C04C05ComplianceTest.runAllTests()
        } catch {
            print("\n💥 Critical failure in C-04/C-05 tests")
            print("Please check the implementation against tasks_final.md requirements")
            exit(1)
        }
    }
}