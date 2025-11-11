import Foundation

/// MemoryBridge - Local memory orchestration following arectiure_final.md
/// Integrates EmbedRunner, SQLiteStore, and VecIndex for on-device memory operations
/// Architecture compliant: Single Swift process with local embeddings
public final class MemoryBridge {

    // MARK: - Properties

    private let embedRunner: EmbedRunner
    private let sqliteStore: SQLiteStore
    private let vecIndex: VecIndex

    // MARK: - Performance optimization properties

    /// In-memory cache for recent transcript processing to avoid redundant operations
    private var transcriptCache: [String: MemoryResponse] = [:]
    private let maxTranscriptCacheSize = 50
    private let transcriptCacheQueue = DispatchQueue(label: "com.alfred.memorybridge.transcriptCache")

    // MARK: - Initialization

    /// Initialize bridge with local memory components
    public init(embedRunner: EmbedRunner? = nil,
                sqliteStore: SQLiteStore? = nil) throws {
        let resolvedRunner = try embedRunner ?? EmbedRunner()
        self.embedRunner = resolvedRunner

        let resolvedStore = try sqliteStore ?? SQLiteStore()
        self.sqliteStore = resolvedStore

        // Initialize vector index with the SQLite connection
        self.vecIndex = VecIndex(db: resolvedStore.db)

        print("✅ MemoryBridge: Local memory system initialized")
        print("✅ MemoryBridge: EmbedRunner ready -> \(resolvedRunner.ready)")
    }

    // MARK: - Public API

    /// Process user transcript and return speech plan with memories (optimized)
    /// - Parameter transcript: User's spoken transcript
    /// - Returns: Response with speech plan and retrieved memories
    public func processTranscript(_ transcript: String) async throws -> MemoryResponse {
        let startTime = CFAbsoluteTimeGetCurrent()
        let trimmedTranscript = transcript.trimmingCharacters(in: .whitespacesAndNewlines)

        // Check cache first to avoid redundant processing
        if let cached = cachedResponse(for: trimmedTranscript) {
            print("🎯 MemoryBridge: Cache hit for transcript length \(trimmedTranscript.count)")
            return cached
        }

        do {
            // Generate embedding for the transcript
            let queryEmbedding = try await embedRunner.embed(trimmedTranscript)

            // Search for similar memories using vector index
            let similarMemories = try vecIndex.findSimilarNotes(
                queryEmbedding: queryEmbedding,
                limit: 5,
                threshold: 0.7
            )

            // Get full memory details (optimized)
            let retrievedMemories = try await getFullMemoryDetails(for: similarMemories)

            // Generate speech plan (simplified - in full implementation would call Cerebras)
            let speechPlan = generateSpeechPlan(transcript: trimmedTranscript, memories: retrievedMemories)

            let processingTime = (CFAbsoluteTimeGetCurrent() - startTime) * 1000
            let response = MemoryResponse(
                speechPlan: speechPlan,
                retrievedMemories: retrievedMemories,
                processingTimeMs: processingTime
            )

            // Cache the response
            cacheTranscriptResponse(transcript: trimmedTranscript, response: response)

            return response

        } catch {
            print("❌ MemoryBridge process failed: \(error)")
            // Return fallback response
            let fallbackResponse = MemoryResponse(
                speechPlan: "I heard you say: \(trimmedTranscript)",
                retrievedMemories: [],
                processingTimeMs: (CFAbsoluteTimeGetCurrent() - startTime) * 1000
            )

            // Cache even fallback responses to avoid repeated failed processing
            cacheTranscriptResponse(transcript: trimmedTranscript, response: fallbackResponse)
            return fallbackResponse
        }
    }

    /// Add a new memory to the store
    /// - Parameters:
    ///   - content: Memory content
    ///   - metadata: Optional metadata dictionary
    /// - Returns: UUID of the created memory
    public func addMemory(_ content: String, metadata: [String: Any] = [:]) async throws -> String {
        do {
            // Generate embedding for the memory
            let embedding = try await embedRunner.embed(content)

            // Add memory to SQLite store
            let note = try sqliteStore.addNote(content: content, metadata: serializeMetadata(metadata))

            // Store embedding in vector index
            try vecIndex.storeEmbedding(noteId: note.id, embedding: embedding)

            print("✅ MemoryBridge: Added memory \(note.uuid)")
            return note.uuid

        } catch {
            print("❌ MemoryBridge addMemory failed: \(error)")
            throw MemoryBridgeError.operationFailed("Failed to add memory: \(error.localizedDescription)")
        }
    }

    /// Search memories by semantic similarity
    /// - Parameters:
    ///   - query: Search query
    ///   - limit: Maximum number of results
    /// - Returns: Array of memory dictionaries with similarity scores
    public func searchMemories(_ query: String, limit: Int = 5) async throws -> [MemoryResult] {
        do {
            // Generate embedding for query
            let queryEmbedding = try await embedRunner.embed(query)

            // Search vector index
            let similarMemories = try vecIndex.findSimilarNotes(
                queryEmbedding: queryEmbedding,
                limit: limit,
                threshold: 0.5
            )

            // Convert to MemoryResult format
            return try await getFullMemoryDetails(for: similarMemories)

        } catch {
            print("❌ MemoryBridge search failed: \(error)")
            return []
        }
    }

    /// Get memory statistics
    /// - Returns: Dictionary with memory store statistics
    public func getMemoryStats() async throws -> [String: Any] {
        do {
            let noteCount = try sqliteStore.getNotesCount()
            return [
                "total_notes": noteCount,
                "embedding_dimension": EmbedRunner.embeddingDimension,
                "model_ready": embedRunner.ready
            ]
        } catch {
            print("❌ MemoryBridge stats failed: \(error)")
            return [:]
        }
    }

    // MARK: - Private Methods

    /// Get full memory details for vector search results (optimized)
    private func getFullMemoryDetails(for similarMemories: [(noteId: Int, similarity: Float)]) async throws -> [MemoryResult] {
        var results: [MemoryResult] = []

        // Use efficient database lookups instead of loading all notes
        for memory in similarMemories {
            // Get specific note by ID - much more efficient
            if let note = try sqliteStore.getNoteByID(id: memory.noteId) {
                let metadata = deserializeMetadata(note.metadata)
                results.append(MemoryResult(
                    content: note.content,
                    similarity: memory.similarity,
                    metadata: metadata,
                    uuid: note.uuid
                ))
            }
        }

        return results.sorted { $0.similarity > $1.similarity }
    }

    /// Generate speech plan based on transcript and memories
    /// In full implementation, this would call Cerebras API
    private func generateSpeechPlan(transcript: String, memories: [MemoryResult]) -> String {
        guard !memories.isEmpty else {
            return "I heard you say: \(transcript)"
        }

        let memorySummary = memories.prefix(2).map { memory in
            "- \(memory.content.prefix(50))..."
        }.joined(separator: "\n")

        return """
        I heard you say: "\(transcript)"

        I found some relevant memories:
        \(memorySummary)

        How can I help you with this?
        """
    }

    /// Serialize metadata dictionary to JSON string
    private func serializeMetadata(_ metadata: [String: Any]) -> String {
        guard let data = try? JSONSerialization.data(withJSONObject: metadata),
              let string = String(data: data, encoding: .utf8) else {
            return "{}"
        }
        return string
    }

    /// Deserialize metadata JSON string to dictionary
    private func deserializeMetadata(_ metadata: String?) -> [String: Any] {
        guard let metadata = metadata,
              let data = metadata.data(using: .utf8),
              let dict = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else {
            return [:]
        }
        return dict
    }

    /// Cache transcript processing response
    private func cacheTranscriptResponse(transcript: String, response: MemoryResponse) {
        transcriptCacheQueue.sync {
            // Remove oldest entries if cache is full
            if self.transcriptCache.count >= self.maxTranscriptCacheSize {
                let keysToRemove = Array(self.transcriptCache.keys.prefix(self.maxTranscriptCacheSize / 4))
                for key in keysToRemove {
                    self.transcriptCache.removeValue(forKey: key)
                }
            }

            self.transcriptCache[transcript] = response
        }
    }

    /// Clear transcript cache (useful for testing or memory management)
    public func clearTranscriptCache() {
        transcriptCacheQueue.sync {
            self.transcriptCache.removeAll()
        }
        print("🧹 MemoryBridge: Cleared transcript cache")
    }

    /// Delete a stored memory by UUID (used for diagnostics/self-tests)
    public func deleteMemory(uuid: String) throws {
        try sqliteStore.deleteNote(uuid: uuid)
    }

    private func cachedResponse(for transcript: String) -> MemoryResponse? {
        transcriptCacheQueue.sync {
            transcriptCache[transcript]
        }
    }
}

// MARK: - Supporting Models

public struct MemoryResponse {
    public let speechPlan: String
    public let retrievedMemories: [MemoryResult]
    public let processingTimeMs: Double
}

public struct MemoryResult {
    public let content: String
    public let similarity: Float
    public let metadata: [String: Any]
    public let uuid: String
}

// MARK: - Error Types

public enum MemoryBridgeError: Error, LocalizedError {
    case initializationFailed(String)
    case operationFailed(String)
    case embeddingFailed(String)
    case databaseError(String)

    public var errorDescription: String? {
        switch self {
        case .initializationFailed(let message):
            return "Initialization failed: \(message)"
        case .operationFailed(let message):
            return "Operation failed: \(message)"
        case .embeddingFailed(let message):
            return "Embedding failed: \(message)"
        case .databaseError(let message):
            return "Database error: \(message)"
        }
    }
}
