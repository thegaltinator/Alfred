import Foundation

/// MemoryBridge - Simple subprocess bridge to Python memory orchestrator
/// Replaces the entire Swift memory stack with efficient Python calls
public final class MemoryBridge {

    // MARK: - Properties

    private let pythonPath: String
    private let orchestratorPath: String
    private let session = URLSession.shared

    // MARK: - Initialization

    /// Initialize bridge to Python memory orchestrator
    public init() throws {
        // Find Python executable
        self.pythonPath = try Self.findPythonExecutable()

        // Path to memory orchestrator module
        let clientDir = URL(fileURLWithPath: #file)
            .deletingLastPathComponent()  // Memory/
            .deletingLastPathComponent()  // client/
        self.orchestratorPath = clientDir
            .appendingPathComponent("memory_orchestrator")
            .path

        // Verify orchestrator exists
        let orchestratorInit = "\(orchestratorPath)/__init__.py"
        guard FileManager.default.fileExists(atPath: orchestratorInit) else {
            throw MemoryBridgeError.orchestratorNotFound("Memory orchestrator not found at \(orchestratorPath)")
        }

        print("✅ MemoryBridge: Python -> \(pythonPath)")
        print("✅ MemoryBridge: Orchestrator -> \(orchestratorPath)")
    }

    // MARK: - Public API

    /// Process user transcript and return speech plan with memories
    /// - Parameter transcript: User's spoken transcript
    /// - Returns: Response with speech plan and retrieved memories
    public func processTranscript(_ transcript: String) async throws -> MemoryResponse {
        let startTime = CFAbsoluteTimeGetCurrent()

        do {
            let result = try await runPythonCommand(
                "process",
                args: [transcript]
            )

            let response = try parseMemoryResponse(result)

            let processingTime = (CFAbsoluteTimeGetCurrent() - startTime) * 1000
            return MemoryResponse(
                speechPlan: response.speech_plan,
                retrievedMemories: response.retrieved_memories,
                processingTimeMs: processingTime
            )

        } catch {
            print("❌ MemoryBridge process failed: \(error)")
            // Return fallback response
            return MemoryResponse(
                speechPlan: "I heard you say: \(transcript)",
                retrievedMemories: [],
                processingTimeMs: (CFAbsoluteTimeGetCurrent() - startTime) * 1000
            )
        }
    }

    /// Add a new memory to the store
    /// - Parameters:
    ///   - content: Memory content
    ///   - metadata: Optional metadata dictionary
    /// - Returns: UUID of the created memory
    public func addMemory(_ content: String, metadata: [String: Any] = [:]) async throws -> String {
        do {
            let metadataString = try dictToJsonString(metadata)
            let result = try await runPythonCommand(
                "add",
                args: [content, "--metadata", metadataString]
            )
            return result.trimmingCharacters(in: .whitespacesAndNewlines)
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
            let result = try await runPythonCommand(
                "search",
                args: [query, "--limit", String(limit)]
            )

            guard let data = result.data(using: .utf8),
                  let memories = try JSONSerialization.jsonObject(with: data) as? [[String: Any]] else {
                return []
            }

            return memories.compactMap { memoryDict in
                guard let content = memoryDict["content"] as? String,
                      let similarity = memoryDict["similarity"] as? Double else {
                    return nil
                }

                return MemoryResult(
                    content: content,
                    similarity: similarity,
                    metadata: memoryDict["metadata"] as? [String: Any] ?? [:],
                    uuid: memoryDict["uuid"] as? String ?? UUID().uuidString
                )
            }

        } catch {
            print("❌ MemoryBridge search failed: \(error)")
            return []
        }
    }

    // MARK: - Private Methods

    private func runPythonCommand(_ command: String, args: [String]) async throws -> String {
        let process = Process()
        process.executableURL = URL(fileURLWithPath: pythonPath)

        // Build Python command
        var pythonArgs = [
            "-m", "memory_orchestrator",
            command
        ]
        pythonArgs.append(contentsOf: args)

        // Set working directory to client directory
        let clientDir = URL(fileURLWithPath: orchestratorPath).deletingLastPathComponent()
        process.currentDirectoryURL = clientDir

        process.arguments = pythonArgs

        // Set up environment
        var environment = ProcessInfo.processInfo.environment
        environment["PYTHONPATH"] = clientDir.path
        process.environment = environment

        let stdoutPipe = Pipe()
        let stderrPipe = Pipe()
        process.standardOutput = stdoutPipe
        process.standardError = stderrPipe

        // Run process with timeout
        try process.run()

        let waitResult = try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Void, Error>) in
            DispatchQueue.global().async {
                process.waitUntilExit()
                if process.terminationStatus == 0 {
                    continuation.resume()
                } else {
                    let stderrData = stderrPipe.fileHandleForReading.readDataToEndOfFile()
                    let stderrOutput = String(data: stderrData, encoding: .utf8) ?? "Unknown error"
                    continuation.resume(throwing: MemoryBridgeError.processFailed(status: process.terminationStatus, message: stderrOutput))
                }
            }
        }

        // Get output
        let stdoutData = stdoutPipe.fileHandleForReading.readDataToEndOfFile()
        guard let output = String(data: stdoutData, encoding: .utf8) else {
            throw MemoryBridgeError.outputParsingFailed("Failed to decode Python output")
        }

        return output
    }

    private func parseMemoryResponse(_ jsonString: String) throws -> MemoryResponseInternal {
        guard let data = jsonString.data(using: .utf8) else {
            throw MemoryBridgeError.outputParsingFailed("Invalid UTF-8 output")
        }

        let decoder = JSONDecoder()
        return try decoder.decode(MemoryResponseInternal.self, from: data)
    }

    private func dictToJsonString(_ dict: [String: Any]) throws -> String {
        guard let data = try? JSONSerialization.data(withJSONObject: dict),
              let string = String(data: data, encoding: .utf8) else {
            throw MemoryBridgeError.invalidInput("Cannot convert metadata to JSON")
        }
        return string
    }

    private static func findPythonExecutable() throws -> String {
        // Try common Python executable names
        let candidates = [
            "/usr/bin/python3",
            "/usr/local/bin/python3",
            "/opt/homebrew/bin/python3",
            "python3"  // Will search PATH
        ]

        for candidate in candidates {
            if candidate.hasPrefix("/") {
                // Absolute path - check if exists and is executable
                if FileManager.default.isExecutableFile(atPath: candidate) {
                    return candidate
                }
            } else {
                // Search in PATH
                if let whichResult = try? shell("which \(candidate)"),
                   !whichResult.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                    return whichResult.trimmingCharacters(in: .whitespacesAndNewlines)
                }
            }
        }

        throw MemoryBridgeError.pythonNotFound("Python3 executable not found")
    }

    private static func shell(_ command: String) throws -> String {
        let process = Process()
        process.executableURL = URL(fileURLWithPath: "/bin/bash")
        process.arguments = ["-c", command]

        let pipe = Pipe()
        process.standardOutput = pipe

        try process.run()
        process.waitUntilExit()

        let data = pipe.fileHandleForReading.readDataToEndOfFile()
        return String(data: data, encoding: .utf8) ?? ""
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
    public let similarity: Double
    public let metadata: [String: Any]
    public let uuid: String
}

// Internal response model for JSON parsing
private struct MemoryResponseInternal: Codable {
    let speech_plan: String
    let retrieved_memories: [MemoryResultInternal]
    let processing_time_ms: Double
    let error: String?

    private struct MemoryResultInternal: Codable {
        let content: String
        let similarity: Double
        let metadata: [String: String]  // Simplified for JSON
        let uuid: String?
    }
}

// MARK: - Error Types

public enum MemoryBridgeError: Error, LocalizedError {
    case pythonNotFound(String)
    case orchestratorNotFound(String)
    case processFailed(status: Int32, message: String)
    case outputParsingFailed(String)
    case invalidInput(String)
    case operationFailed(String)

    public var errorDescription: String? {
        switch self {
        case .pythonNotFound(let message):
            return "Python not found: \(message)"
        case .orchestratorNotFound(let message):
            return "Memory orchestrator not found: \(message)"
        case .processFailed(let status, let message):
            return "Python process failed with status \(status): \(message)"
        case .outputParsingFailed(let message):
            return "Failed to parse Python output: \(message)"
        case .invalidInput(let message):
            return "Invalid input: \(message)"
        case .operationFailed(let message):
            return "Operation failed: \(message)"
        }
    }
}