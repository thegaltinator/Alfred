import Foundation
import SQLite3

/// VecIndex - sqlite-vec / sqlite-vss glue for 1024-dim cosine similarity
/// Provides vector search capabilities for semantic memory recall
public class VecIndex {

    // MARK: - Properties

    /// Database connection (shared with SQLiteStore)
    private let db: OpaquePointer?

    /// Thread safety queue
    private let queue = DispatchQueue(label: "com.alfred.vecindex", qos: .utility)

    /// Vector dimension (1024 for Qwen3-Embedding-0.6B)
    private static let vectorDimension = 1024

    // MARK: - Initialization

    /// Initialize vector index with shared database connection
    /// - Parameter db: SQLite database connection
    public init(db: OpaquePointer?) {
        self.db = db
        createVectorTables()
    }

    // MARK: - Table Setup

    /// Create vector tables for embedding storage and search
    private func createVectorTables() {
        do {
            // Create embeddings table with vector columns
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

            // Initialize sqlite-vec extension (placeholder for C-04)
            // In full implementation, this would load the sqlite-vec extension
            print("✅ VecIndex: Vector tables created/verified")

        } catch {
            print("❌ VecIndex: Failed to create vector tables: \(error.localizedDescription)")
        }
    }

    // MARK: - Vector Operations

    /// Store embedding vector for a note
    /// - Parameters:
    ///   - noteId: ID of the note
    ///   - embedding: 1024-dimensional embedding vector as [Float]
    public func storeEmbedding(noteId: Int, embedding: [Float]) throws {
        guard embedding.count == Self.vectorDimension else {
            throw VecIndexError.invalidDimension("Expected \(Self.vectorDimension), got \(embedding.count)")
        }

        try queue.sync {
            // Convert Float array to Data (blob representation)
            let data = embedding.withUnsafeBufferPointer { buffer in
                Data(buffer: buffer)
            }

            let sql = """
            INSERT OR REPLACE INTO embeddings (note_id, embedding)
            VALUES (?, ?);
            """

            var stmt: OpaquePointer?
            let prepareResult = sqlite3_prepare_v2(db, sql, -1, &stmt, nil)

            guard prepareResult == SQLITE_OK else {
                throw VecIndexError.preparationFailed("Failed to prepare embedding insert")
            }

            defer { sqlite3_finalize(stmt) }

            sqlite3_bind_int(stmt, 1, Int32(noteId))
            data.withUnsafeBytes { bytes in
                if let baseAddress = bytes.baseAddress {
                    sqlite3_bind_blob(stmt, 2, baseAddress, Int32(data.count), nil)
                }
            }

            let stepResult = sqlite3_step(stmt)
            guard stepResult == SQLITE_DONE else {
                let errorMsg = String(cString: sqlite3_errmsg(db))
                throw VecIndexError.executionFailed("Embedding insert failed: \(errorMsg)")
            }
        }

        print("✅ VecIndex: Stored embedding for note \(noteId)")
    }

    /// Find similar notes using cosine similarity
    /// - Parameters:
    ///   - queryEmbedding: Query embedding vector
    ///   - limit: Maximum number of results to return
    ///   - threshold: Minimum similarity threshold (0.0 to 1.0)
    /// - Returns: Array of similar note IDs with similarity scores
    public func findSimilarNotes(queryEmbedding: [Float], limit: Int = 5, threshold: Float = 0.7) throws -> [(noteId: Int, similarity: Float)] {
        guard queryEmbedding.count == Self.vectorDimension else {
            throw VecIndexError.invalidDimension("Expected \(Self.vectorDimension), got \(queryEmbedding.count)")
        }

        return try queue.sync {
            // For C-04, implement basic cosine similarity calculation
            // In full implementation, this would use sqlite-vec for efficient vector search

            let sql = """
            SELECT note_id, embedding FROM embeddings
            WHERE note_id IN (
                SELECT id FROM notes WHERE is_deleted = 0
            );
            """

            var stmt: OpaquePointer?
            let prepareResult = sqlite3_prepare_v2(db, sql, -1, &stmt, nil)

            guard prepareResult == SQLITE_OK else {
                throw VecIndexError.preparationFailed("Failed to prepare similarity search")
            }

            defer { sqlite3_finalize(stmt) }

            var results: [(noteId: Int, similarity: Float)] = []

            while sqlite3_step(stmt) == SQLITE_ROW {
                let noteId = Int(sqlite3_column_int(stmt, 0))

                // Extract embedding blob
                guard let blobPointer = sqlite3_column_blob(stmt, 1) else {
                    continue
                }
                let blobSize = Int(sqlite3_column_bytes(stmt, 1))
                guard blobSize == MemoryLayout<Float>.size * Self.vectorDimension else {
                    continue
                }

                // Convert blob to Float array
                let data = Data(bytes: blobPointer, count: blobSize)
                let storedEmbedding = data.withUnsafeBytes { buffer -> [Float] in
                    let floatBuffer = buffer.bindMemory(to: Float.self)
                    return Array(floatBuffer)
                }

                // Calculate cosine similarity
                let similarity = cosineSimilarity(queryEmbedding, storedEmbedding)

                if similarity >= threshold {
                    results.append((noteId: noteId, similarity: similarity))
                }
            }

            // Sort by similarity (descending) and limit results
            results.sort { $0.similarity > $1.similarity }
            return Array(results.prefix(limit))
        }
    }

    /// Calculate cosine similarity between two vectors
    /// - Parameters:
    ///   - vecA: First vector
    ///   - vecB: Second vector
    /// - Returns: Cosine similarity score (-1.0 to 1.0)
    private func cosineSimilarity(_ vecA: [Float], _ vecB: [Float]) -> Float {
        guard vecA.count == vecB.count else { return 0.0 }

        var dotProduct: Float = 0.0
        var normA: Float = 0.0
        var normB: Float = 0.0

        for i in 0..<vecA.count {
            dotProduct += vecA[i] * vecB[i]
            normA += vecA[i] * vecA[i]
            normB += vecB[i] * vecB[i]
        }

        guard normA > 0 && normB > 0 else { return 0.0 }

        return dotProduct / (sqrt(normA) * sqrt(normB))
    }

    /// Delete embeddings for a note
    /// - Parameter noteId: ID of the note
    public func deleteEmbeddings(noteId: Int) throws {
        try queue.sync {
            let sql = "DELETE FROM embeddings WHERE note_id = ?;"

            var stmt: OpaquePointer?
            let prepareResult = sqlite3_prepare_v2(db, sql, -1, &stmt, nil)

            guard prepareResult == SQLITE_OK else {
                throw VecIndexError.preparationFailed("Failed to prepare embedding delete")
            }

            defer { sqlite3_finalize(stmt) }

            sqlite3_bind_int(stmt, 1, Int32(noteId))

            let stepResult = sqlite3_step(stmt)
            guard stepResult == SQLITE_DONE else {
                let errorMsg = String(cString: sqlite3_errmsg(db))
                throw VecIndexError.executionFailed("Embedding delete failed: \(errorMsg)")
            }
        }

        print("✅ VecIndex: Deleted embeddings for note \(noteId)")
    }

    // MARK: - Database Operations

    /// Execute SQL statement with error handling
    private func executeSQL(_ sql: String) throws {
        var errMsg: UnsafeMutablePointer<Int8>?
        let result = sqlite3_exec(db, sql, nil, nil, &errMsg)

        if result != SQLITE_OK {
            let errorMsg = errMsg != nil ? String(cString: errMsg!) : "Unknown SQL error"
            sqlite3_free(errMsg)
            throw VecIndexError.executionFailed(errorMsg)
        }
    }
}

// MARK: - Error Types

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
