import XCTest
import Foundation
@testable import Memory

/// Unit tests for SQLiteStore functionality
/// Tests database operations without mocking - uses real SQLite in temporary directory
class SQLiteStoreTests: XCTestCase {

    // MARK: - Properties

    private var testStore: SQLiteStore!
    private var testDir: URL!

    // MARK: - Test Lifecycle

    override func setUp() {
        super.setUp()

        // Create temporary directory for test database
        testDir = FileManager.default.temporaryDirectory.appendingPathComponent("AlfredTest_\(UUID().uuidString)")
        try! FileManager.default.createDirectory(at: testDir, withIntermediateDirectories: true)

        // Override the database path for testing by setting a custom directory
        // We'll create a modified version of SQLiteStore for testing
        testStore = try! SQLiteStore(testURL: testDir.appendingPathComponent("test_memory.db"))
    }

    override func tearDown() {
        super.tearDown()

        // Clean up test database and directory
        testStore = nil
        try! FileManager.default.removeItem(at: testDir)
    }

    // MARK: - Database Creation Tests

    func testDatabaseCreation() throws {
        // Verify database file exists
        let dbURL = testDir.appendingPathComponent("test_memory.db")
        XCTAssertTrue(FileManager.default.fileExists(atPath: dbURL.path),
                     "Database file should be created at the specified path")

        // Verify WAL mode is enabled
        let countBefore = try testStore.getNotesCount()
        XCTAssertEqual(countBefore, 0, "Initial database should have 0 notes")
    }

    // MARK: - Note Addition Tests

    func testAddNote() throws {
        // Add a simple note
        let content = "Test note content"
        let note = try testStore.addNote(content: content)

        // Verify note was added
        XCTAssertFalse(note.uuid.isEmpty, "Note UUID should not be empty")
        XCTAssertGreaterThan(note.id, 0, "Note ID should be populated")
        let count = try testStore.getNotesCount()
        XCTAssertEqual(count, 1, "Should have 1 note after adding one")

        // Add another note with metadata
        let metadata = "{\"tags\": [\"test\", \"important\"]}"
        let note2 = try testStore.addNote(content: "Second note", metadata: metadata)
        XCTAssertFalse(note2.uuid.isEmpty, "Second note UUID should not be empty")
        XCTAssertNotEqual(note.uuid, note2.uuid, "Notes should have different UUIDs")
        XCTAssertNotEqual(note.id, note2.id, "Notes should have different IDs")

        let count2 = try testStore.getNotesCount()
        XCTAssertEqual(count2, 2, "Should have 2 notes after adding second")
    }

    func testAddNoteWithEmptyContent() throws {
        // Test that empty content is allowed (as per requirements)
        let note = try testStore.addNote(content: "")
        XCTAssertFalse(note.uuid.isEmpty, "Empty content note should still be created")

        let count = try testStore.getNotesCount()
        XCTAssertEqual(count, 1, "Empty content note should count towards total")
    }

    func testAddNoteWithLargeContent() throws {
        // Test large content
        let largeContent = String(repeating: "This is a test sentence. ", count: 100)
        let note = try testStore.addNote(content: largeContent)

        XCTAssertFalse(note.uuid.isEmpty, "Large content note should be created")
        let count = try testStore.getNotesCount()
        XCTAssertEqual(count, 1, "Large content note should be stored")
    }

    // MARK: - Note Retrieval Tests

    func testGetRecentNotes() throws {
        // Add multiple notes
        let note1 = try testStore.addNote(content: "First note")
        Thread.sleep(forTimeInterval: 0.01) // Ensure different timestamps
        let note2 = try testStore.addNote(content: "Second note")
        Thread.sleep(forTimeInterval: 0.01)
        let note3 = try testStore.addNote(content: "Third note")

        // Get all recent notes (default limit 10)
        let allNotes = try testStore.getRecentNotes()
        XCTAssertEqual(allNotes.count, 3, "Should retrieve all 3 notes")

        // Verify notes are in descending order (newest first)
        XCTAssertEqual(allNotes[0].uuid, note3.uuid, "First note should be the most recent")
        XCTAssertEqual(allNotes[1].uuid, note2.uuid, "Second note should be the middle one")
        XCTAssertEqual(allNotes[2].uuid, note1.uuid, "Third note should be the oldest")

        // Test limit
        let limitedNotes = try testStore.getRecentNotes(limit: 2)
        XCTAssertEqual(limitedNotes.count, 2, "Should respect limit parameter")
        XCTAssertEqual(limitedNotes[0].uuid, note3.uuid, "Limited notes should start with most recent")
    }

    func testGetRecentNotesWithOffset() throws {
        // Add multiple notes
        let note1 = try testStore.addNote(content: "First")
        let note2 = try testStore.addNote(content: "Second")
        _ = try testStore.addNote(content: "Third")

        // Test offset
        let offsetNotes = try testStore.getRecentNotes(limit: 10, offset: 1)
        XCTAssertEqual(offsetNotes.count, 2, "Offset should skip first note")
        XCTAssertEqual(offsetNotes[0].uuid, note2.uuid, "Offset notes should start with second note")
    }

    func testGetRecentNotesEmpty() throws {
        // Test with empty database
        let emptyNotes = try testStore.getRecentNotes()
        XCTAssertEqual(emptyNotes.count, 0, "Empty database should return no notes")
    }

    // MARK: - Note Deletion Tests

    func testSoftDeleteNote() throws {
        // Add notes
        let note1 = try testStore.addNote(content: "To be deleted")
        let note2 = try testStore.addNote(content: "To keep")

        XCTAssertEqual(try testStore.getNotesCount(), 2, "Should have 2 notes initially")

        // Soft delete one note
        try testStore.deleteNote(uuid: note1.uuid)

        // Count should be 1 (deleted notes excluded)
        XCTAssertEqual(try testStore.getNotesCount(), 1, "Should have 1 note after soft delete")

        // Recent notes should only show the non-deleted one
        let recentNotes = try testStore.getRecentNotes()
        XCTAssertEqual(recentNotes.count, 1, "Should only show non-deleted notes")
        XCTAssertEqual(recentNotes[0].uuid, note2.uuid, "Should show the correct remaining note")
    }

    func testDeleteNonExistentNote() throws {
        // Try to delete a note that doesn't exist
        try testStore.deleteNote(uuid: "non-existent-uuid")

        // Should not crash and count should remain 0
        XCTAssertEqual(try testStore.getNotesCount(), 0, "Count should remain 0")
    }

    // MARK: - Edge Cases and Error Handling

    func testConcurrentAccess() throws {
        // Test concurrent access from multiple threads
        let expectation = XCTestExpectation(description: "Concurrent note addition")
        expectation.expectedFulfillmentCount = 10

        for i in 0..<10 {
            DispatchQueue.global().async {
                do {
                    _ = try self.testStore.addNote(content: "Concurrent note \(i)")
                } catch {
                    XCTFail("Concurrent note addition failed: \(error)")
                }
                expectation.fulfill()
            }
        }

        wait(for: [expectation], timeout: 5.0)

        // Verify all notes were added
        let finalCount = try testStore.getNotesCount()
        XCTAssertEqual(finalCount, 10, "All concurrent notes should be added")
    }

    func testNoteContentWithSpecialCharacters() throws {
        // Test with special characters, emojis, and unicode
        let specialContent = "Note with émojis 🤖, special chars: !@#$%^&*(), and unicode: 你好世界"
        let note = try testStore.addNote(content: specialContent)

        let notes = try testStore.getRecentNotes()
        XCTAssertEqual(notes.count, 1, "Special content note should be stored")
        XCTAssertEqual(notes[0].content, specialContent, "Content should be preserved exactly")
        XCTAssertEqual(notes[0].uuid, note.uuid, "UUID should match")
    }

    func testMetadataStorage() throws {
        // Test metadata storage and retrieval
        let metadata = "{\"source\": \"test\", \"priority\": \"high\", \"tags\": [\"work\", \"important\"]}"
        let note = try testStore.addNote(content: "Test note with metadata", metadata: metadata)

        let notes = try testStore.getRecentNotes()
        XCTAssertEqual(notes[0].metadata, metadata, "Metadata should be stored and retrieved correctly")
        XCTAssertEqual(notes[0].uuid, note.uuid, "UUID should be consistent")
    }

    func testVectorSearchWithStoredEmbeddings() throws {
        let primary = try testStore.addNote(content: "Swift concurrency essentials")
        let distraction = try testStore.addNote(content: "Golang goroutines overview")
        let related = try testStore.addNote(content: "Async/await patterns in Swift")

        var embeddingPrimary = Array(repeating: Float(0), count: EmbedRunner.embeddingDimension)
        embeddingPrimary[0] = 1.0

        var embeddingDistraction = Array(repeating: Float(0), count: EmbedRunner.embeddingDimension)
        embeddingDistraction[1] = 1.0

        var embeddingRelated = Array(repeating: Float(0), count: EmbedRunner.embeddingDimension)
        let component: Float = Float(1.0 / sqrt(2.0))
        embeddingRelated[0] = component
        embeddingRelated[1] = component

        try testStore.storeEmbedding(noteId: primary.id, embedding: embeddingPrimary)
        try testStore.storeEmbedding(noteId: distraction.id, embedding: embeddingDistraction)
        try testStore.storeEmbedding(noteId: related.id, embedding: embeddingRelated)

        let queryEmbedding = embeddingPrimary
        let matches = try testStore.findSimilarNotes(for: queryEmbedding, limit: 3, threshold: 0.2)

        XCTAssertEqual(matches.count, 2, "Only notes with cosine similarity >= 0.2 should be returned")
        XCTAssertEqual(matches[0].note.uuid, primary.uuid, "Exact match should rank first")
        XCTAssertGreaterThan(matches[0].similarity, 0.99, "Self similarity should be ~1.0")
        XCTAssertEqual(matches[1].note.uuid, related.uuid, "Related note should rank second")
        XCTAssertGreaterThan(matches[1].similarity, 0.6, "Related note should have meaningful similarity")
    }
}

// MARK: - Test Extension

/// Extension to SQLiteStore for testing with custom database path
extension SQLiteStore {
    /// Initialize SQLiteStore with custom database URL for testing
    init(testURL: URL) throws {
        dbURL = testURL
        try openDatabase()
        try createTables()
    }
}
