import Foundation
import SQLite3

/// SQLite-based memory store for notes and preferences with WAL mode for write optimization
/// Thread-safe database operations for Alfred's local memory system
public class SQLiteStore {

    // MARK: - Properties

    /// Database connection handle
    private(set) var db: OpaquePointer?

    /// Database file URL
    private let dbURL: URL

    /// Thread safety queue
    private let queue = DispatchQueue(label: "com.alfred.memorystore", qos: .utility)

    /// Shared vector index for embedding persistence/search
    private var vecIndex: VecIndex?

    // MARK: - Initialization

    /// Initialize memory store with database in Application Support directory
    public init() throws {
        let appSupportURL = FileManager.default.urls(for: .applicationSupportDirectory,
                                                     in: .userDomainMask).first!
        let alfredDir = appSupportURL.appendingPathComponent("Alfred")

        // Create Alfred directory if it doesn't exist
        try FileManager.default.createDirectory(at: alfredDir,
                                                withIntermediateDirectories: true)

        dbURL = alfredDir.appendingPathComponent("memory.db")

        try openDatabase()
        try createTables()
    }

    deinit {
        closeDatabase()
    }

    // MARK: - Database Operations

    /// Open database connection with WAL mode enabled
    private func openDatabase() throws {
        var db: OpaquePointer?

        // Open database with WAL mode and optimal settings for our use case
        let result = sqlite3_open_v2(dbURL.path, &db,
                                    SQLITE_OPEN_READWRITE | SQLITE_OPEN_CREATE,
                                    nil)

        guard result == SQLITE_OK else {
            let errorMsg = String(cString: sqlite3_errmsg(db))
            sqlite3_close(db)
            throw SQLiteError.openFailed(errorMsg)
        }

        self.db = db
        self.vecIndex = VecIndex(db: db)

        // Enable WAL mode for better write performance and concurrency
        try executeSQL("PRAGMA journal_mode=WAL;")
        try executeSQL("PRAGMA synchronous=NORMAL;")
        try executeSQL("PRAGMA cache_size=10000;")
        try executeSQL("PRAGMA temp_store=MEMORY;")

        print("✅ SQLiteStore: Database opened at \(dbURL.path) with WAL mode")
    }

    /// Close database connection
    private func closeDatabase() {
        queue.sync {
            if let db = db {
                sqlite3_close(db)
                self.db = nil
                self.vecIndex = nil
            }
        }
    }

    /// Create necessary tables for memory storage
    private func createTables() throws {
        let createNotesTable = """
        CREATE TABLE IF NOT EXISTS notes (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            uuid TEXT UNIQUE NOT NULL,
            content TEXT NOT NULL,
            created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
            updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
            metadata TEXT, -- JSON metadata for tags, sources, etc.
            is_deleted BOOLEAN DEFAULT 0
        );

        CREATE INDEX IF NOT EXISTS idx_notes_created_at ON notes(created_at);
        CREATE INDEX IF NOT EXISTS idx_notes_uuid ON notes(uuid);
        """

        try executeSQL(createNotesTable)
        print("✅ SQLiteStore: Notes table created/verified")
    }

    /// Execute SQL statement with error handling
    private func executeSQL(_ sql: String) throws {
        try queue.sync {
            var errMsg: UnsafeMutablePointer<Int8>?
            let result = sqlite3_exec(db, sql, nil, nil, &errMsg)

            if result != SQLITE_OK {
                let errorMsg = errMsg != nil ? String(cString: errMsg!) : "Unknown SQL error"
                sqlite3_free(errMsg)
                throw SQLiteError.executionFailed(errorMsg)
            }
        }
    }

    // MARK: - Public Interface

    /// Add a new note to the memory store
    /// - Parameters:
    ///   - content: The note content text
    ///   - metadata: Optional JSON metadata string
    /// - Returns: Created Note record
    public func addNote(content: String, metadata: String? = nil) throws -> Note {
        var uuid: String

        // Ensure we get a unique UUID with retry logic
        repeat {
            uuid = UUID().uuidString
        } while try noteExists(uuid: uuid)

        var insertedNote: Note?

        try queue.sync {
            let sql = """
            INSERT INTO notes (uuid, content, metadata)
            VALUES (?, ?, ?);
            """

            var stmt: OpaquePointer?
            let prepareResult = sqlite3_prepare_v2(db, sql, -1, &stmt, nil)

            guard prepareResult == SQLITE_OK else {
                throw SQLiteError.preparationFailed("Failed to prepare INSERT statement")
            }

            defer { sqlite3_finalize(stmt) }

            sqlite3_bind_text(stmt, 1, uuid, -1, nil)
            sqlite3_bind_text(stmt, 2, content, -1, nil)
            sqlite3_bind_text(stmt, 3, metadata, -1, nil)

            let stepResult = sqlite3_step(stmt)
            guard stepResult == SQLITE_DONE else {
                let errorMsg = String(cString: sqlite3_errmsg(db))
                throw SQLiteError.executionFailed("INSERT failed: \(errorMsg)")
            }

            let fetchSQL = """
            SELECT id, uuid, content, created_at, updated_at, metadata
            FROM notes
            WHERE id = last_insert_rowid();
            """

            var fetchStmt: OpaquePointer?
            let fetchPrepare = sqlite3_prepare_v2(db, fetchSQL, -1, &fetchStmt, nil)

            guard fetchPrepare == SQLITE_OK else {
                throw SQLiteError.preparationFailed("Failed to prepare INSERT fetch statement")
            }

            defer { sqlite3_finalize(fetchStmt) }

            if sqlite3_step(fetchStmt) == SQLITE_ROW {
                let id = Int(sqlite3_column_int(fetchStmt, 0))
                let uuidCStr = sqlite3_column_text(fetchStmt, 1)
                let contentCStr = sqlite3_column_text(fetchStmt, 2)
                let createdCStr = sqlite3_column_text(fetchStmt, 3)
                let updatedCStr = sqlite3_column_text(fetchStmt, 4)
                let metadataCStr = sqlite3_column_text(fetchStmt, 5)

                insertedNote = Note(
                    id: id,
                    uuid: uuidCStr != nil ? String(cString: uuidCStr!) : uuid,
                    content: contentCStr != nil ? String(cString: contentCStr!) : content,
                    createdAt: createdCStr != nil ? String(cString: createdCStr!) : "",
                    updatedAt: updatedCStr != nil ? String(cString: updatedCStr!) : "",
                    metadata: metadataCStr != nil ? String(cString: metadataCStr!) : metadata
                )
            }
        }

        print("✅ SQLiteStore: Added note with UUID: \(uuid)")
        guard let note = insertedNote else {
            throw SQLiteError.recordNotFound("Inserted note \(uuid) could not be fetched")
        }
        return note
    }

    /// Check if a note with the given UUID already exists
    /// - Parameter uuid: UUID to check
    /// - Returns: True if note exists, false otherwise
    private func noteExists(uuid: String) throws -> Bool {
        return try queue.sync {
            let sql = "SELECT COUNT(*) FROM notes WHERE uuid = ?;"

            var stmt: OpaquePointer?
            let prepareResult = sqlite3_prepare_v2(db, sql, -1, &stmt, nil)

            guard prepareResult == SQLITE_OK else {
                throw SQLiteError.preparationFailed("Failed to prepare UUID check statement")
            }

            defer { sqlite3_finalize(stmt) }

            sqlite3_bind_text(stmt, 1, uuid, -1, nil)

            let stepResult = sqlite3_step(stmt)
            guard stepResult == SQLITE_ROW else {
                throw SQLiteError.executionFailed("Failed to execute UUID check")
            }

            return sqlite3_column_int(stmt, 0) > 0
        }
    }

    /// Get total count of notes in the database
    /// - Returns: Number of non-deleted notes
    public func getNotesCount() throws -> Int {
        return try queue.sync {
            let sql = "SELECT COUNT(*) FROM notes WHERE is_deleted = 0;"

            var stmt: OpaquePointer?
            let prepareResult = sqlite3_prepare_v2(db, sql, -1, &stmt, nil)

            guard prepareResult == SQLITE_OK else {
                throw SQLiteError.preparationFailed("Failed to prepare COUNT statement")
            }

            defer { sqlite3_finalize(stmt) }

            let stepResult = sqlite3_step(stmt)
            guard stepResult == SQLITE_ROW else {
                throw SQLiteError.executionFailed("Failed to execute COUNT query")
            }

            return Int(sqlite3_column_int(stmt, 0))
        }
    }

    /// Get recent notes for display
    /// - Parameters:
    ///   - limit: Maximum number of notes to return
    ///   - offset: Offset for pagination
    /// - Returns: Array of Note objects
    public func getRecentNotes(limit: Int = 10, offset: Int = 0) throws -> [Note] {
        return try queue.sync {
            let sql = """
            SELECT id, uuid, content, created_at, updated_at, metadata
            FROM notes
            WHERE is_deleted = 0
            ORDER BY created_at DESC
            LIMIT ? OFFSET ?;
            """

            var stmt: OpaquePointer?
            let prepareResult = sqlite3_prepare_v2(db, sql, -1, &stmt, nil)

            guard prepareResult == SQLITE_OK else {
                throw SQLiteError.preparationFailed("Failed to prepare SELECT statement")
            }

            defer { sqlite3_finalize(stmt) }

            sqlite3_bind_int(stmt, 1, Int32(limit))
            sqlite3_bind_int(stmt, 2, Int32(offset))

            var notes: [Note] = []

            while sqlite3_step(stmt) == SQLITE_ROW {
                let id = sqlite3_column_int(stmt, 0)
                let uuidCStr = sqlite3_column_text(stmt, 1)
                let uuid = uuidCStr != nil ? String(cString: uuidCStr!) : ""
                let contentCStr = sqlite3_column_text(stmt, 2)
                let content = contentCStr != nil ? String(cString: contentCStr!) : ""
                let createdCStr = sqlite3_column_text(stmt, 3)
                let created = createdCStr != nil ? String(cString: createdCStr!) : ""
                let updatedCStr = sqlite3_column_text(stmt, 4)
                let updated = updatedCStr != nil ? String(cString: updatedCStr!) : ""
                let metadataCStr = sqlite3_column_text(stmt, 5)
                let metadata = metadataCStr != nil ? String(cString: metadataCStr!) : nil

                let note = Note(
                    id: Int(id),
                    uuid: uuid,
                    content: content,
                    createdAt: created,
                    updatedAt: updated,
                    metadata: metadata
                )
                notes.append(note)
            }

            return notes
        }
    }

    /// Mark a note as deleted (soft delete)
    /// - Parameter uuid: UUID of the note to delete
    public func deleteNote(uuid: String) throws {
        guard let note = try getNoteByUUID(uuid: uuid) else {
            print("⚠️ SQLiteStore: Attempted to delete unknown note: \(uuid)")
            return
        }

        try queue.sync {
            let sql = "UPDATE notes SET is_deleted = 1, updated_at = CURRENT_TIMESTAMP WHERE uuid = ?;"

            var stmt: OpaquePointer?
            let prepareResult = sqlite3_prepare_v2(db, sql, -1, &stmt, nil)

            guard prepareResult == SQLITE_OK else {
                throw SQLiteError.preparationFailed("Failed to prepare DELETE statement")
            }

            defer { sqlite3_finalize(stmt) }

            sqlite3_bind_text(stmt, 1, uuid, -1, nil)

            let stepResult = sqlite3_step(stmt)
            guard stepResult == SQLITE_DONE else {
                let errorMsg = String(cString: sqlite3_errmsg(db))
                throw SQLiteError.executionFailed("DELETE failed: \(errorMsg)")
            }
        }

        print("✅ SQLiteStore: Soft deleted note: \(uuid)")

        do {
            let index = try requireVecIndex()
            try index.deleteEmbeddings(noteId: note.id)
        } catch {
            print("❌ SQLiteStore: Failed to delete embeddings for note \(uuid): \(error.localizedDescription)")
            throw error
        }
    }

    /// Store embedding vector for a note
    /// - Parameters:
    ///   - noteId: Database ID of the note
    ///   - embedding: Embedding vector (1024-dim)
    public func storeEmbedding(noteId: Int, embedding: [Float]) throws {
        let index = try requireVecIndex()
        try index.storeEmbedding(noteId: noteId, embedding: embedding)
    }

    /// Find similar notes for a given embedding vector
    /// - Parameters:
    ///   - embedding: Query embedding vector
    ///   - limit: Maximum number of matches to return
    ///   - threshold: Minimum cosine similarity threshold
    /// - Returns: Array of matching notes with similarity
    public func findSimilarNotes(for embedding: [Float], limit: Int = 5, threshold: Float = 0.7) throws -> [(note: Note, similarity: Float)] {
        let index = try requireVecIndex()
        let matches = try index.findSimilarNotes(queryEmbedding: embedding, limit: limit, threshold: threshold)

        var results: [(Note, Float)] = []
        for match in matches {
            if let note = try getNoteByID(id: match.noteId) {
                results.append((note, match.similarity))
            }
        }
        return results
    }

    /// Retrieve a note by UUID
    /// - Parameter uuid: Note UUID
    /// - Returns: Note if found
    public func getNoteByUUID(uuid: String) throws -> Note? {
        return try queue.sync {
            let sql = """
            SELECT id, uuid, content, created_at, updated_at, metadata, is_deleted
            FROM notes
            WHERE uuid = ?;
            """

            var stmt: OpaquePointer?
            let prepareResult = sqlite3_prepare_v2(db, sql, -1, &stmt, nil)

            guard prepareResult == SQLITE_OK else {
                throw SQLiteError.preparationFailed("Failed to prepare UUID lookup")
            }

            defer { sqlite3_finalize(stmt) }

            sqlite3_bind_text(stmt, 1, uuid, -1, nil)

            guard sqlite3_step(stmt) == SQLITE_ROW else {
                return nil
            }

            return Note(
                id: Int(sqlite3_column_int(stmt, 0)),
                uuid: uuid,
                content: sqlite3_column_text(stmt, 2).flatMap { String(cString: $0) } ?? "",
                createdAt: sqlite3_column_text(stmt, 3).flatMap { String(cString: $0) } ?? "",
                updatedAt: sqlite3_column_text(stmt, 4).flatMap { String(cString: $0) } ?? "",
                metadata: sqlite3_column_text(stmt, 5).flatMap { String(cString: $0) },
                isDeleted: sqlite3_column_int(stmt, 6) == 1
            )
        }
    }

    /// Retrieve a note by database ID
    /// - Parameter id: Note ID
    /// - Returns: Note if found and not deleted
    public func getNoteByID(id: Int) throws -> Note? {
        return try queue.sync {
            let sql = """
            SELECT id, uuid, content, created_at, updated_at, metadata, is_deleted
            FROM notes
            WHERE id = ?;
            """

            var stmt: OpaquePointer?
            let prepareResult = sqlite3_prepare_v2(db, sql, -1, &stmt, nil)

            guard prepareResult == SQLITE_OK else {
                throw SQLiteError.preparationFailed("Failed to prepare ID lookup")
            }

            defer { sqlite3_finalize(stmt) }

            sqlite3_bind_int(stmt, 1, Int32(id))

            guard sqlite3_step(stmt) == SQLITE_ROW else {
                return nil
            }

            return Note(
                id: id,
                uuid: sqlite3_column_text(stmt, 1).flatMap { String(cString: $0) } ?? "",
                content: sqlite3_column_text(stmt, 2).flatMap { String(cString: $0) } ?? "",
                createdAt: sqlite3_column_text(stmt, 3).flatMap { String(cString: $0) } ?? "",
                updatedAt: sqlite3_column_text(stmt, 4).flatMap { String(cString: $0) } ?? "",
                metadata: sqlite3_column_text(stmt, 5).flatMap { String(cString: $0) },
                isDeleted: sqlite3_column_int(stmt, 6) == 1
            )
        }
    }

    /// Ensure vector index is initialized before use
    private func requireVecIndex() throws -> VecIndex {
        guard let index = vecIndex else {
            throw SQLiteError.vectorIndexUnavailable("Vector index not initialized")
        }
        return index
    }
}

// MARK: - Data Models

public struct Note {
    public let id: Int
    public let uuid: String
    public let content: String
    public let createdAt: String
    public let updatedAt: String
    public let metadata: String?
    public let isDeleted: Bool

    public init(id: Int, uuid: String, content: String, createdAt: String, updatedAt: String, metadata: String?, isDeleted: Bool = false) {
        self.id = id
        self.uuid = uuid
        self.content = content
        self.createdAt = createdAt
        self.updatedAt = updatedAt
        self.metadata = metadata
        self.isDeleted = isDeleted
    }
}

// MARK: - Error Types

public enum SQLiteError: Error, LocalizedError {
    case openFailed(String)
    case preparationFailed(String)
    case executionFailed(String)
    case recordNotFound(String)
    case vectorIndexUnavailable(String)

    public var errorDescription: String? {
        switch self {
        case .openFailed(let message):
            return "Failed to open database: \(message)"
        case .preparationFailed(let message):
            return "Failed to prepare SQL statement: \(message)"
        case .executionFailed(let message):
            return "Failed to execute SQL: \(message)"
        case .recordNotFound(let message):
            return "Record not found: \(message)"
        case .vectorIndexUnavailable(let message):
            return "Vector index unavailable: \(message)"
        }
    }
}
