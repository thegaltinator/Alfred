import Foundation

/// Extension for array chunking used in batch processing
extension Array {
    func chunked(into size: Int) -> [[Element]] {
        return stride(from: 0, to: count, by: size).map {
            Array(self[$0..<Swift.min($0 + size, count)])
        }
    }
}

/// EmbedRunner - Qwen3-Embedding-0.6B inference via direct library integration.
/// Provides ultra-fast embeddings for Alfred's semantic memory index.
/// Architecture compliant: Uses GGUF via llama.cpp/CoreML as specified in arectiure_final.md
/// Performance optimized: Direct model loading with sub-50ms embedding times
public final class EmbedRunner {

    // MARK: - Static configuration

    /// Expected dimensionality for Qwen3-Embedding-0.6B vectors.
    public static let embeddingDimension = 1024

    private static let candidateBinaryNames = ["llama-embedding", "llama-cli", "llama", "main"]

    // MARK: - Properties

    private let modelPath: String
    private let directEmbedRunner: DirectEmbedRunner
    private let queue = DispatchQueue(label: "com.alfred.embedrunner", qos: .userInitiated)

    // MARK: - Performance optimization properties

    /// In-memory cache for recent embeddings to avoid recomputation
    private var embeddingCache: [String: [Float]] = [:]
    private let maxCacheSize = 100
    private let cacheQueue = DispatchQueue(label: "com.alfred.embedrunner.cache")

    // MARK: - Initialization

    /// Initialize runner with direct model loading.
    /// - Parameter modelPath: Optional explicit path to Qwen3-Embedding-0.6B GGUF file.
    public init(modelPath: String? = nil) throws {
        self.modelPath = try Self.resolveModelPath(preferredPath: modelPath)
        self.directEmbedRunner = try DirectEmbedRunner(modelPath: self.modelPath)

        print("✅ EmbedRunner: Model -> \(self.modelPath)")
        print("✅ EmbedRunner: Direct model loading enabled")
        print("⚡ EmbedRunner: Ready for sub-50ms embeddings")
    }

    // MARK: - Public API

    /// Generate embedding for supplied text using direct model inference.
    /// - Parameter text: Input content (must be non-empty after trimming).
    /// - Returns: 1024-dimensional embedding vector.
    public func embed(_ text: String) async throws -> [Float] {
        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)

        guard !trimmed.isEmpty else {
            throw EmbedRunnerError.emptyInput("Cannot embed empty text")
        }

        // Check cache first for performance
        if let cachedVector = cachedEmbedding(for: trimmed) {
            print("🎯 EmbedRunner: Cache hit for text length \(trimmed.count)")
            return cachedVector
        }

        // Use direct embedding runner for ultra-fast inference
        let startTime = CFAbsoluteTimeGetCurrent()
        let vector = try await directEmbedRunner.embed(trimmed)
        let embeddingTime = (CFAbsoluteTimeGetCurrent() - startTime) * 1000

        print("⚡ Direct embedding: \(String(format: "%.2f", embeddingTime))ms")

        // Cache the result for future use
        cacheEmbedding(text: trimmed, vector: vector)

        return vector
    }

    /// Efficiently embed multiple texts in a single batch request using direct model.
    /// - Parameter texts: Array of input strings.
    /// - Returns: Array of embeddings (one per input).
    public func embedBatch(_ texts: [String]) async throws -> [[Float]] {
        guard !texts.isEmpty else { return [] }

        // Filter out empty texts
        let validTexts = texts.filter { !$0.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty }
        guard !validTexts.isEmpty else { return [] }

        // Check cache for all texts first
        var uncachedTexts: [String] = []
        var cachedResults: [Int: [Float]] = [:]

        cacheQueue.sync {
            for (index, text) in validTexts.enumerated() {
                if let cachedVector = embeddingCache[text] {
                    cachedResults[index] = cachedVector
                } else {
                    uncachedTexts.append(text)
                }
            }
        }

        // Process uncached texts using direct batch processing (much faster)
        let startTime = CFAbsoluteTimeGetCurrent()
        let uncachedResults = try await directEmbedRunner.embedBatch(uncachedTexts)
        let batchTime = (CFAbsoluteTimeGetCurrent() - startTime) * 1000

        print("⚡ Direct batch embedding: \(String(format: "%.2f", batchTime))ms for \(uncachedTexts.count) texts")

        // Combine cached and new results
        var outputs: [[Float]] = Array(repeating: [], count: validTexts.count)
        var uncachedIndex = 0

        for (index, _) in validTexts.enumerated() {
            if let cached = cachedResults[index] {
                outputs[index] = cached
            } else {
                if uncachedIndex < uncachedResults.count {
                    outputs[index] = uncachedResults[uncachedIndex]
                    uncachedIndex += 1
                }
            }
        }

        // Cache new results
        for (index, text) in validTexts.enumerated() {
            if cachedResults[index] == nil && uncachedIndex > 0 {
                cacheEmbedding(text: text, vector: outputs[index])
            }
        }

        return outputs.filter { !$0.isEmpty }
    }

    /// Quick readiness flag.
    public var ready: Bool {
        return directEmbedRunner.ready
    }

    /// Model metadata.
    public var modelInfo: ModelInfo {
        directEmbedRunner.modelInfo
    }

    /// Unload model resources.
    public func unloadModel() {
        queue.async {
            self.directEmbedRunner.unloadModel()
            print("🔄 EmbedRunner: Direct model unloaded")
        }
    }

    // MARK: - Cache Management

    /// Cache an embedding result
    private func cacheEmbedding(text: String, vector: [Float]) {
        cacheQueue.sync {
            if embeddingCache.count >= maxCacheSize {
                let keysToRemove = Array(embeddingCache.keys.prefix(maxCacheSize / 4))
                for key in keysToRemove {
                    embeddingCache.removeValue(forKey: key)
                }
            }
            embeddingCache[text] = vector
        }
    }

    private func cachedEmbedding(for text: String) -> [Float]? {
        cacheQueue.sync {
            embeddingCache[text]
        }
    }

    // MARK: - Path Resolution Helpers

    private static let preferredModelFilenames = [
        "Qwen3-Embedding-0.6B-f16.gguf",
        "Qwen3-Embedding-0.6B-Q8_0.gguf"
    ]

    private static func resolveModelPath(preferredPath: String?) throws -> String {
        let candidates = orderedCandidates(from: [
            preferredPath,
            ProcessInfo.processInfo.environment["ALFRED_EMBED_MODEL_PATH"],
            defaultAppSupportModelPath()
        ])

        let fileManager = FileManager.default
        for candidate in orderedUnique(candidates) {
            if fileManager.fileExists(atPath: candidate) {
                return candidate
            }
        }

        throw EmbedRunnerError.modelNotFound("Qwen3-Embedding-0.6B model (GGUF) not found. Checked: \(candidates.joined(separator: ", "))")
    }

    private static func defaultAppSupportModelPath() -> String? {
        let appSupportURL = FileManager.default.urls(for: .applicationSupportDirectory,
                                                     in: .userDomainMask).first!
        let alfredDir = appSupportURL.appendingPathComponent("Alfred/Models")

        for filename in preferredModelFilenames {
            let candidatePath = alfredDir.appendingPathComponent(filename).path
            if FileManager.default.fileExists(atPath: candidatePath) {
                return candidatePath
            }
        }

        return alfredDir.path
    }

    private static func orderedCandidates(from inputs: [String?]) -> [String] {
        var results: [String] = []
        for entry in inputs {
            if let entry = entry {
                results.append(entry)
            }
        }
        return results
    }

    private static func orderedUnique(_ elements: [String]) -> [String] {
        var seen = Set<String>()
        return elements.filter { seen.insert($0).inserted }
    }

    private static func expandTilde(_ path: String) -> String {
        if path.hasPrefix("~") {
            let home = FileManager.default.homeDirectoryForCurrentUser.path
            return String(path.dropFirst()).replacingOccurrences(of: "~", with: home)
        }
        return path
    }

    // MARK: - Static Factory Methods

    /// Initialize runner with resolved model + binary locations.
    public static func create(modelPath: String? = nil,
                             binaryPath: String? = nil) throws -> EmbedRunner {
        return try EmbedRunner(modelPath: modelPath)
    }

    /// Check if the model and binary are available.
    public static func isAvailable() -> Bool {
        do {
            let _ = try EmbedRunner()
            return true
        } catch {
            return false
        }
    }

    /// Check if a model file exists at the given path.
    public static func modelExists(at path: String) -> Bool {
        return FileManager.default.fileExists(atPath: path)
    }
}

// MARK: - Supporting Models

/// Model metadata structure
public struct ModelInfo {
    public let name: String
    public let dimension: Int
    public let version: String
}

// MARK: - Error Types

public enum EmbedRunnerError: Error, LocalizedError {
    case modelNotFound(String)
    case binaryNotFound(String)
    case emptyInput(String)
    case modelNotLoaded(String)
    case processExecutionFailed(String)
    case processTimedOut(String)
    case processFailed(status: Int32, stderr: String)
    case outputParsingFailed(String)
    case embeddingGenerationFailed(String)

    public var errorDescription: String? {
        switch self {
        case .modelNotFound(let message):
            return "Model not found: \(message)"
        case .binaryNotFound(let message):
            return "Binary not found: \(message)"
        case .emptyInput(let message):
            return "Empty input: \(message)"
        case .modelNotLoaded(let message):
            return "Model not loaded: \(message)"
        case .processExecutionFailed(let message):
            return "Process execution failed: \(message)"
        case .processTimedOut(let message):
            return "Process timed out: \(message)"
        case .processFailed(let status, let stderr):
            return "Process failed (status \(status)): \(stderr)"
        case .outputParsingFailed(let message):
            return "Output parsing failed: \(message)"
        case .embeddingGenerationFailed(let message):
            return "Embedding generation failed: \(message)"
        }
    }
}
