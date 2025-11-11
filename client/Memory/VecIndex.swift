import Foundation
import SQLite3
import CSqliteVec

private let SQLITE_TRANSIENT = unsafeBitCast(-1, to: sqlite3_destructor_type.self)

/// VecIndex - sqlite-vec glue for 1024-dim cosine similarity
public final class VecIndex {
    private let db: OpaquePointer?
    private let queue = DispatchQueue(label: "com.alfred.vecindex", qos: .utility)
    private static let vectorDimension = 1024
    private static let registrationLock = NSLock()
    private static var registered = false

    public init(db: OpaquePointer?) {
        self.db = db
        registerVecExtension()
        createTables()
    }

    public func storeEmbedding(noteId: Int, embedding: [Float]) throws {
        guard embedding.count == Self.vectorDimension else {
            throw VecIndexError.invalidDimension("Expected \(Self.vectorDimension), got \(embedding.count)")
        }

        try queue.sync {
            try insertBlobEmbedding(noteId: noteId, embedding: embedding)
            try insertVectorRow(noteId: noteId, embedding: embedding)
        }

        print("✅ VecIndex: Stored embedding for note \(noteId)")
    }

    public func findSimilarNotes(queryEmbedding: [Float], limit: Int = 5, threshold: Float = 0.7) throws -> [(noteId: Int, similarity: Float)] {
        guard queryEmbedding.count == Self.vectorDimension else {
            throw VecIndexError.invalidDimension("Expected \(Self.vectorDimension), got \(queryEmbedding.count)")
        }

        return try queue.sync {
            try searchWithVectorExtension(queryEmbedding: queryEmbedding, limit: limit, threshold: threshold)
        }
    }

    public func deleteEmbeddings(noteId: Int) throws {
        try queue.sync {
            let deleteBlob = "DELETE FROM embeddings WHERE note_id = ?;"
            var stmt: OpaquePointer?
            guard sqlite3_prepare_v2(db, deleteBlob, -1, &stmt, nil) == SQLITE_OK else {
                throw VecIndexError.preparationFailed("Failed to prepare embedding delete")
            }
            defer { sqlite3_finalize(stmt) }
            sqlite3_bind_int(stmt, 1, Int32(noteId))
            guard sqlite3_step(stmt) == SQLITE_DONE else {
                let errorMsg = String(cString: sqlite3_errmsg(db))
                throw VecIndexError.executionFailed("Embedding delete failed: \(errorMsg)")
            }

            let deleteVec = "DELETE FROM vec_index WHERE rowid = ?;"
            var vecStmt: OpaquePointer?
            guard sqlite3_prepare_v2(db, deleteVec, -1, &vecStmt, nil) == SQLITE_OK else {
                throw VecIndexError.preparationFailed("Failed to prepare vec_index delete")
            }
            defer { sqlite3_finalize(vecStmt) }
            sqlite3_bind_int(vecStmt, 1, Int32(noteId))
            guard sqlite3_step(vecStmt) == SQLITE_DONE else {
                let errorMsg = String(cString: sqlite3_errmsg(db))
                throw VecIndexError.executionFailed("vec_index delete failed: \(errorMsg)")
            }
        }

        print("✅ VecIndex: Deleted embeddings for note \(noteId)")
    }

    // MARK: - Private helpers

    private func registerVecExtension() {
        Self.registrationLock.lock()
        defer { Self.registrationLock.unlock() }
        guard !Self.registered else { return }
        guard let db else { fatalError("VecIndex: missing database handle for sqlite-vec registration") }
        let rc = sqlite_vec_register(db)
        guard rc == SQLITE_OK else {
            fatalError("VecIndex: sqlite-vec registration failed (rc \(rc))")
        }
        Self.registered = true
        print("✅ VecIndex: sqlite-vec extension registered")
    }

    private func createTables() {
        do {
            let createEmbeddingsTable = """
            CREATE TABLE IF NOT EXISTS embeddings (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                note_id INTEGER NOT NULL,
                embedding BLOB NOT NULL,
                created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
                FOREIGN KEY (note_id) REFERENCES notes(id) ON DELETE CASCADE
            );

            CREATE UNIQUE INDEX IF NOT EXISTS idx_embeddings_note_id ON embeddings(note_id);
            """

            try executeSQL(createEmbeddingsTable)

            let createVecTable = """
            CREATE VIRTUAL TABLE IF NOT EXISTS vec_index USING vec0(
                embedding float[\(Self.vectorDimension)] distance_metric=cosine
            );
            """

            try executeSQL(createVecTable)
            print("✅ VecIndex: Vector tables created/verified")
        } catch {
            fatalError("❌ VecIndex: Failed to create vector tables: \(error.localizedDescription)")
        }
    }

    private func insertBlobEmbedding(noteId: Int, embedding: [Float]) throws {
        guard let db else { throw VecIndexError.executionFailed("Database unavailable") }
        let data = embedding.withUnsafeBufferPointer { Data(buffer: $0) }
        let sql = "INSERT OR REPLACE INTO embeddings (note_id, embedding) VALUES (?, ?);"
        var stmt: OpaquePointer?
        guard sqlite3_prepare_v2(db, sql, -1, &stmt, nil) == SQLITE_OK else {
            throw VecIndexError.preparationFailed("Failed to prepare embedding insert")
        }
        defer { sqlite3_finalize(stmt) }
        sqlite3_bind_int(stmt, 1, Int32(noteId))
        data.withUnsafeBytes { bytes in
            if let baseAddress = bytes.baseAddress {
                sqlite3_bind_blob(stmt, 2, baseAddress, Int32(data.count), nil)
            }
        }
        guard sqlite3_step(stmt) == SQLITE_DONE else {
            let errorMsg = String(cString: sqlite3_errmsg(db))
            throw VecIndexError.executionFailed("Embedding insert failed: \(errorMsg)")
        }
    }

    private func insertVectorRow(noteId: Int, embedding: [Float]) throws {
        guard let db else { throw VecIndexError.executionFailed("Database unavailable") }
        let sql = "INSERT OR REPLACE INTO vec_index(rowid, embedding) VALUES (?, ?);"
        var stmt: OpaquePointer?
        guard sqlite3_prepare_v2(db, sql, -1, &stmt, nil) == SQLITE_OK else {
            throw VecIndexError.preparationFailed("Failed to prepare vec_index insert")
        }
        defer { sqlite3_finalize(stmt) }
        sqlite3_bind_int(stmt, 1, Int32(noteId))
        let json = try jsonString(for: embedding)
        sqlite3_bind_text(stmt, 2, json, -1, SQLITE_TRANSIENT)
        guard sqlite3_step(stmt) == SQLITE_DONE else {
            let errorMsg = String(cString: sqlite3_errmsg(db))
            throw VecIndexError.executionFailed("vec_index insert failed: \(errorMsg)")
        }
    }

    private func searchWithVectorExtension(queryEmbedding: [Float], limit: Int, threshold: Float) throws -> [(noteId: Int, similarity: Float)] {
        guard let db else { throw VecIndexError.executionFailed("Database unavailable") }
        let json = try jsonString(for: queryEmbedding)
        let sql = """
        SELECT rowid, distance
        FROM vec_index
        WHERE embedding MATCH ?
        ORDER BY distance
        LIMIT ?;
        """

        var stmt: OpaquePointer?
        guard sqlite3_prepare_v2(db, sql, -1, &stmt, nil) == SQLITE_OK else {
            throw VecIndexError.preparationFailed("Failed to prepare vec search")
        }
        defer { sqlite3_finalize(stmt) }

        sqlite3_bind_text(stmt, 1, json, -1, SQLITE_TRANSIENT)
        sqlite3_bind_int(stmt, 2, Int32(limit))

        var results: [(Int, Float)] = []
        while sqlite3_step(stmt) == SQLITE_ROW {
            let noteId = Int(sqlite3_column_int(stmt, 0))
            let distance = sqlite3_column_double(stmt, 1)
            let similarity = max(0, 1 - Float(distance))
            if similarity >= threshold {
                results.append((noteId, similarity))
            }
        }

        return results
    }

    private func executeSQL(_ sql: String) throws {
        guard let db else { throw VecIndexError.executionFailed("Database unavailable") }
        var errMsg: UnsafeMutablePointer<Int8>?
        let result = sqlite3_exec(db, sql, nil, nil, &errMsg)
        if result != SQLITE_OK {
            let errorMsg = errMsg != nil ? String(cString: errMsg!) : "Unknown SQL error"
            sqlite3_free(errMsg)
            throw VecIndexError.executionFailed(errorMsg)
        }
    }

    private func jsonString(for embedding: [Float]) throws -> String {
        let doubles = embedding.map { Double($0) }
        let data = try JSONSerialization.data(withJSONObject: doubles)
        guard let json = String(data: data, encoding: .utf8) else {
            throw VecIndexError.executionFailed("Failed to encode embedding JSON")
        }
        return json
    }
}

public enum VecIndexError: Error, LocalizedError {
    case invalidDimension(String)
    case preparationFailed(String)
    case executionFailed(String)

    public var errorDescription: String? {
        switch self {
        case .invalidDimension(let message):
            return "Invalid vector dimension: \(message)"
        case .preparationFailed(let message):
            return "Failed to prepare SQL statement: \(message)"
        case .executionFailed(let message):
            return "Failed to execute SQL: \(message)"
        }
    }
}
