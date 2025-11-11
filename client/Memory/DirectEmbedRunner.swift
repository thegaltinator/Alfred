import Foundation

/// Direct llama.cpp bridge used by EmbedRunner.
/// Loads libllama dynamically, keeps the Qwen3-Embedding-0.6B model resident,
/// and services inference requests on a dedicated queue to avoid thread thrash.
public final class DirectEmbedRunner: @unchecked Sendable {

    // MARK: - Static configuration

    private static let defaultModelFilename = "Qwen3-Embedding-0.6B-f16.gguf"
    private static let alternateModelFilename = "Qwen3-Embedding-0.6B-Q8_0.gguf"

    // MARK: - Properties

    private let modelPath: String
    private let libraryPath: String
    private let queue = DispatchQueue(label: "com.alfred.directembed", qos: .userInitiated)

    private var session: LlamaSession?
    private let sessionLock = NSLock()

    private lazy var modelMetadata: ModelInfo = {
        ModelInfo(
            name: "Qwen3-Embedding-0.6B (llama.cpp)",
            dimension: session?.embeddingDimension ?? 1024,
            version: "1.0"
        )
    }()

    // MARK: - Initialization

    public init(modelPath: String? = nil, libraryPath: String? = nil) throws {
        self.modelPath = try Self.resolveModelPath(preferredPath: modelPath)
        self.libraryPath = try Self.resolveLibraryPath(preferredPath: libraryPath)

        print("🚀 DirectEmbedRunner: loading Qwen model")
        print("   ↳ model: \(self.modelPath)")
        print("   ↳ libllama: \(self.libraryPath)")

        self.session = try LlamaSession(
            modelPath: self.modelPath,
            libraryPath: self.libraryPath,
            configuration: LlamaSessionConfiguration.makeDefault()
        )

        print("✅ DirectEmbedRunner: llama.cpp session ready")
    }

    // MARK: - Public API

    public func embed(_ text: String) async throws -> [Float] {
        return try await withCheckedThrowingContinuation { continuation in
            queue.async {
                do {
                    let vector = try self.activeSession().embed(text: text)
                    continuation.resume(returning: vector)
                } catch {
                    continuation.resume(throwing: error)
                }
            }
        }
    }

    public func embedBatch(_ texts: [String]) async throws -> [[Float]] {
        guard !texts.isEmpty else { return [] }
        var outputs: [[Float]] = []
        for text in texts {
            let vector = try await embed(text)
            outputs.append(vector)
        }
        return outputs
    }

    public var ready: Bool {
        sessionLock.lock()
        defer { sessionLock.unlock() }
        return session != nil
    }

    public var modelInfo: ModelInfo {
        sessionLock.lock()
        defer { sessionLock.unlock() }
        var info = modelMetadata
        if let dimension = session?.embeddingDimension {
            info = ModelInfo(name: info.name, dimension: dimension, version: info.version)
        }
        return info
    }

    public func unloadModel() {
        sessionLock.lock()
        session = nil
        sessionLock.unlock()
        print("🧹 DirectEmbedRunner: released llama.cpp session")
    }

    // MARK: - Helpers

    private static func resolveModelPath(preferredPath: String?) throws -> String {
        var candidates: [String] = []

        if let preferred = preferredPath, !preferred.isEmpty {
            candidates.append(expandTilde(preferred))
        }

        if let envPath = ProcessInfo.processInfo.environment["ALFRED_EMBED_MODEL_PATH"] {
            candidates.append(expandTilde(envPath))
        }

        let appSupport = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask).first!
        let baseDir = appSupport.appendingPathComponent("Alfred/Models")
        candidates.append(baseDir.appendingPathComponent(defaultModelFilename).path)
        candidates.append(baseDir.appendingPathComponent(alternateModelFilename).path)

        for path in orderedUnique(candidates) {
            if FileManager.default.fileExists(atPath: path) {
                return path
            }
        }

        throw EmbedRunnerError.modelNotLoaded("Qwen3-Embedding-0.6B GGUF not found. Checked: \(candidates.joined(separator: ", "))")
    }

    private static func resolveLibraryPath(preferredPath: String?) throws -> String {
        var candidates: [String] = []

        if let preferred = preferredPath, !preferred.isEmpty {
            candidates.append(expandTilde(preferred))
        }

        if let envPath = ProcessInfo.processInfo.environment["ALFRED_LLAMA_LIB_PATH"] {
            candidates.append(expandTilde(envPath))
        }

        let appSupport = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask).first!
        candidates.append(appSupport.appendingPathComponent("Alfred/lib/libllama.dylib").path)

        candidates.append("/usr/local/lib/libllama.dylib")
        candidates.append("/opt/homebrew/lib/libllama.dylib")
        candidates.append("/usr/lib/libllama.dylib")
        candidates.append("./libllama.dylib")

        for path in orderedUnique(candidates) {
            if FileManager.default.fileExists(atPath: path) {
                return path
            }
        }

        throw LlamaRuntimeError.libraryNotFound(candidates)
    }

    private static func expandTilde(_ path: String) -> String {
        guard path.hasPrefix("~") else { return path }
        let home = FileManager.default.homeDirectoryForCurrentUser.path
        let suffix = path.dropFirst()
        return home + suffix
    }

    private static func orderedUnique(_ elements: [String]) -> [String] {
        var seen = Set<String>()
        var output: [String] = []
        for element in elements where seen.insert(element).inserted {
            output.append(element)
        }
        return output
    }

    // MARK: - Factory helpers

    public static func create(modelPath: String? = nil, libraryPath: String? = nil) throws -> DirectEmbedRunner {
        return try DirectEmbedRunner(modelPath: modelPath, libraryPath: libraryPath)
    }

    public static func isModelAvailable() -> Bool {
        do {
            _ = try resolveModelPath(preferredPath: nil)
            _ = try resolveLibraryPath(preferredPath: nil)
            return true
        } catch {
            return false
        }
    }

    private func activeSession() throws -> LlamaSession {
        sessionLock.lock()
        defer { sessionLock.unlock() }
        guard let session else {
            throw EmbedRunnerError.modelNotLoaded("llama.cpp session is not available")
        }
        return session
    }
}
