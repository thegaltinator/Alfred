import Foundation

/// EmbedRunner - fast Qwen3-Embedding-0.6B inference via Python helper service.
/// Provides efficient on-device embeddings for Alfred's semantic memory index.
public final class EmbedRunner {

    // MARK: - Static configuration

    /// Expected dimensionality for Qwen3-Embedding-0.6B vectors.
    public static let embeddingDimension = 1024

    // MARK: - Properties

    private let serviceURL: URL
    private let session: URLSession
    private var isModelReady: Bool
    private let queue = DispatchQueue(label: "com.alfred.embedrunner", qos: .userInitiated)

    // MARK: - Initialization

    /// Initialize runner with Python helper service URL.
    /// - Parameters:
    ///   - serviceURL: URL of the Python embedding service.
    ///   - requestTimeout: Maximum seconds to wait for HTTP request.
    public init(serviceURL: URL? = nil,
                requestTimeout: TimeInterval = 30.0) throws {
        // Default to localhost:8901 if not specified
        self.serviceURL = serviceURL ?? URL(string: "http://127.0.0.1:8901")!

        let config = URLSessionConfiguration.default
        config.timeoutIntervalForRequest = requestTimeout
        config.timeoutIntervalForResource = requestTimeout
        self.session = URLSession(configuration: config)

        self.isModelReady = false

        // Check if service is available
        Task {
            await checkServiceHealth()
        }

        print("✅ EmbedRunner: Python service -> \(self.serviceURL)")
    }

    // MARK: - Public API

    /// Generate embedding for supplied text.
    /// - Parameter text: Input content (must be non-empty after trimming).
    /// - Returns: 1024-dimensional embedding vector.
    public func embed(_ text: String) async throws -> [Float] {
        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)

        guard !trimmed.isEmpty else {
            throw EmbedRunnerError.emptyInput("Cannot embed empty text")
        }

        return try await withCheckedThrowingContinuation { continuation in
            Task {
                do {
                    guard await self.isServiceReady() else {
                        continuation.resume(throwing: EmbedRunnerError.modelNotLoaded("Embedding service not ready"))
                        return
                    }

                    let vector = try await self.performEmbedding(for: trimmed)
                    continuation.resume(returning: vector)
                } catch {
                    continuation.resume(throwing: error)
                }
            }
        }
    }

    /// Efficiently embed multiple texts in a single batch request.
    /// - Parameter texts: Array of input strings.
    /// - Returns: Array of embeddings (one per input).
    public func embedBatch(_ texts: [String]) async throws -> [[Float]] {
        guard !texts.isEmpty else { return [] }

        // Filter out empty texts
        let validTexts = texts.filter { !$0.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty }
        guard !validTexts.isEmpty else { return [] }

        return try await withCheckedThrowingContinuation { continuation in
            Task {
                do {
                    guard await self.isServiceReady() else {
                        continuation.resume(throwing: EmbedRunnerError.modelNotLoaded("Embedding service not ready"))
                        return
                    }

                    let embeddings = try await self.performBatchEmbedding(for: validTexts)
                    continuation.resume(returning: embeddings)
                } catch {
                    continuation.resume(throwing: error)
                }
            }
        }
    }

    /// Quick readiness flag.
    public var ready: Bool {
        return isModelReady
    }

    /// Model metadata.
    public var modelInfo: ModelInfo {
        ModelInfo(
            name: "Qwen3-Embedding-0.6B (Python Service)",
            dimension: Self.embeddingDimension,
            isLoaded: isModelReady,
            modelPath: serviceURL.absoluteString,
            binaryPath: "Python HTTP Service"
        )
    }

    /// Flag runner as unloaded.
    public func unloadModel() {
        queue.async {
            self.isModelReady = false
            print("🔄 EmbedRunner: Marked service as unavailable")
        }
    }

    // MARK: - Service Health Checks

    private func checkServiceHealth() async {
        do {
            let url = serviceURL.appending(path: "/health")
            let (data, _) = try await session.data(from: url)

            if let healthResponse = try? JSONDecoder().decode(HealthResponse.self, from: data) {
                await MainActor.run {
                    self.isModelReady = healthResponse.ready && healthResponse.dimension == Self.embeddingDimension
                }
                print("✅ EmbedRunner: Service ready - \(healthResponse.model)")
            } else {
                print("⚠️ EmbedRunner: Service health check failed")
                await MainActor.run {
                    self.isModelReady = false
                }
            }
        } catch {
            print("❌ EmbedRunner: Cannot reach embedding service: \(error)")
            await MainActor.run {
                self.isModelReady = false
            }
        }
    }

    private func isServiceReady() async -> Bool {
        if !isModelReady {
            await checkServiceHealth()
        }
        return isModelReady
    }

    // MARK: - Embedding Execution

    private func performEmbedding(for text: String) async throws -> [Float] {
        let url = serviceURL.appending(path: "/embed")
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")

        let requestBody = EmbeddingRequest(text: text)
        request.httpBody = try JSONEncoder().encode(requestBody)

        let (data, response) = try await session.data(for: request)

        guard let httpResponse = response as? HTTPURLResponse else {
            throw EmbedRunnerError.serviceError("Invalid response from embedding service")
        }

        guard httpResponse.statusCode == 200 else {
            let errorMessage = String(data: data, encoding: .utf8) ?? "Unknown error"
            throw EmbedRunnerError.serviceError("Service error \(httpResponse.statusCode): \(errorMessage)")
        }

        let embeddingResponse = try JSONDecoder().decode(EmbeddingResponse.self, from: data)

        guard embeddingResponse.embedding.count == Self.embeddingDimension else {
            throw EmbedRunnerError.dimensionMismatch(
                "Expected \(Self.embeddingDimension) dimensions, got \(embeddingResponse.embedding.count)"
            )
        }

        return embeddingResponse.embedding
    }

    private func performBatchEmbedding(for texts: [String]) async throws -> [[Float]] {
        let url = serviceURL.appending(path: "/embed_batch")
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")

        let requestBody = BatchEmbeddingRequest(texts: texts)
        request.httpBody = try JSONEncoder().encode(requestBody)

        let (data, response) = try await session.data(for: request)

        guard let httpResponse = response as? HTTPURLResponse else {
            throw EmbedRunnerError.serviceError("Invalid response from embedding service")
        }

        guard httpResponse.statusCode == 200 else {
            let errorMessage = String(data: data, encoding: .utf8) ?? "Unknown error"
            throw EmbedRunnerError.serviceError("Service error \(httpResponse.statusCode): \(errorMessage)")
        }

        let batchResponse = try JSONDecoder().decode(BatchEmbeddingResponse.self, from: data)

        guard batchResponse.embeddings.count == texts.count else {
            throw EmbedRunnerError.serviceError("Expected \(texts.count) embeddings, got \(batchResponse.embeddings.count)")
        }

        // Verify dimensions
        for embedding in batchResponse.embeddings {
            guard embedding.count == Self.embeddingDimension else {
                throw EmbedRunnerError.dimensionMismatch(
                    "Expected \(Self.embeddingDimension) dimensions, got \(embedding.count)"
                )
            }
        }

        return batchResponse.embeddings
    }
}

// MARK: - API Response Models

private struct HealthResponse: Codable {
    let status: String
    let model: String
    let dimension: Int
    let ready: Bool
}

private struct EmbeddingRequest: Codable {
    let text: String
}

private struct EmbeddingResponse: Codable {
    let embedding: [Float]
    let dimension: Int
    let model: String
    let processing_time_ms: Double
}

private struct BatchEmbeddingRequest: Codable {
    let texts: [String]
}

private struct BatchEmbeddingResponse: Codable {
    let embeddings: [[Float]]
    let dimension: Int
    let model: String
    let processing_time_ms: Double
}

// MARK: - Supporting models

/// Information about the active embedding configuration.
public struct ModelInfo {
    public let name: String
    public let dimension: Int
    public let isLoaded: Bool
    public let modelPath: String
    public let binaryPath: String
}

// MARK: - Error types

public enum EmbedRunnerError: Error, LocalizedError {
    case modelNotFound(String)
    case binaryNotFound(String)
    case modelNotLoaded(String)
    case emptyInput(String)
    case serviceError(String)
    case dimensionMismatch(String)
    case timeout(String)
    case embeddingGenerationFailed(String)

    public var errorDescription: String? {
        switch self {
        case .modelNotFound(let message):
            return "Model not found: \(message)"
        case .binaryNotFound(let message):
            return "Binary not found: \(message)"
        case .modelNotLoaded(let message):
            return "Model not loaded: \(message)"
        case .emptyInput(let message):
            return message
        case .serviceError(let message):
            return "Embedding service error: \(message)"
        case .dimensionMismatch(let message):
            return "Embedding dimension mismatch: \(message)"
        case .timeout(let message):
            return "Request timed out: \(message)"
        case .embeddingGenerationFailed(let message):
            return "Embedding generation failed: \(message)"
        }
    }
}