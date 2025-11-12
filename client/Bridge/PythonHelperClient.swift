import Foundation

actor PythonHelperClient {
    struct Options: Codable {
        var topK: Int = 8
        var minScore: Double = 0.28
        var temperature: Double = 0.4
        var stream: Bool = true

        enum CodingKeys: String, CodingKey {
            case topK = "top_k"
            case minScore = "min_score"
            case temperature
            case stream
        }
    }

    struct RequestBody: Codable {
        let sessionID: String
        let userText: String
        let opts: Options

        enum CodingKeys: String, CodingKey {
            case sessionID = "session_id"
            case userText = "user_text"
            case opts
        }
    }

    struct MemoryRef: Decodable {
        let id: Int
        let score: Double
    }

    struct TokenUsage: Decodable {
        let prompt: Int
        let completion: Int
    }

    struct ResponseBody: Decodable {
        let assistantText: String
        let usedMemory: [MemoryRef]
        let latencyMs: Int
        let tokenUsage: TokenUsage

        enum CodingKeys: String, CodingKey {
            case assistantText = "assistant_text"
            case usedMemory = "used_memory"
            case latencyMs = "latency_ms"
            case tokenUsage = "token_usage"
        }
    }

    private struct ErrorEnvelope: Decodable {
        let error: String
    }

    private var process: Process?
    private var stdinPipe: Pipe?
    private var stdoutPipe: Pipe?
    private let helperPath: String
    private let encoder = JSONEncoder()
    private let decoder = JSONDecoder()

    init(scriptPath: String? = nil) {
        if let envPath = ProcessInfo.processInfo.environment["PY_HELPER_PATH"],
           !envPath.isEmpty {
            helperPath = envPath
        } else if let scriptPath {
            helperPath = scriptPath
        } else {
            helperPath = PythonHelperClient.defaultHelperPath().path
        }
    }

    func warmUp() async {
        try? ensureProcess()
    }

    func chat(sessionID: String,
              userText: String,
              options: Options = Options()) async throws -> ResponseBody {
        try ensureProcess()
        guard let stdinHandle = stdinPipe?.fileHandleForWriting else {
            throw PythonHelperError.processUnavailable
        }

        let body = RequestBody(sessionID: sessionID, userText: userText, opts: options)
        let data = try encoder.encode(body)
        stdinHandle.write(data)
        stdinHandle.write(Data([0x0A]))
        stdinHandle.synchronizeFile()

        let line = try readLineFromHelper()
        guard let jsonData = line.data(using: .utf8) else {
            throw PythonHelperError.invalidPayload(line)
        }

        if let envelope = try? decoder.decode(ErrorEnvelope.self, from: jsonData) {
            throw PythonHelperError.helperError(envelope.error)
        }

        return try decoder.decode(ResponseBody.self, from: jsonData)
    }

    private func ensureProcess() throws {
        if let process, process.isRunning {
            return
        }

        let process = Process()
        process.executableURL = URL(fileURLWithPath: "/usr/bin/env")
        process.arguments = ["python3", helperPath]
        let scriptURL = URL(fileURLWithPath: helperPath)
        process.currentDirectoryURL = scriptURL.deletingLastPathComponent()

        let stdinPipe = Pipe()
        let stdoutPipe = Pipe()

        process.standardInput = stdinPipe
        process.standardOutput = stdoutPipe
        process.standardError = FileHandle.standardError

        try process.run()
        self.process = process
        self.stdinPipe = stdinPipe
        self.stdoutPipe = stdoutPipe
    }

    private func readLineFromHelper() throws -> String {
        guard let stdoutHandle = stdoutPipe?.fileHandleForReading else {
            throw PythonHelperError.processUnavailable
        }
        var buffer = Data()
        while true {
            let chunk = stdoutHandle.readData(ofLength: 1)
            if chunk.isEmpty {
                resetProcess()
                throw PythonHelperError.processExited
            }
            if chunk[0] == 0x0A {
                break
            }
            buffer.append(chunk)
        }
        guard let line = String(data: buffer, encoding: .utf8) else {
            throw PythonHelperError.invalidPayload("non-utf8")
        }
        return line
    }

    private func resetProcess() {
        process = nil
        stdinPipe = nil
        stdoutPipe = nil
    }

    private static func defaultHelperPath() -> URL {
        let thisFile = URL(fileURLWithPath: #file)
        let repoRoot = thisFile
            .deletingLastPathComponent() // Bridge
            .deletingLastPathComponent() // client
        return repoRoot.appendingPathComponent("python_helper/app.py")
    }

    enum PythonHelperError: Error, LocalizedError {
        case processUnavailable
        case processExited
        case invalidPayload(String)
        case helperError(String)

        var errorDescription: String? {
            switch self {
            case .processUnavailable:
                return "Python helper subprocess unavailable"
            case .processExited:
                return "Python helper subprocess exited"
            case .invalidPayload(let payload):
                return "Python helper returned invalid payload: \(payload)"
            case .helperError(let message):
                return "Python helper error: \(message)"
            }
        }
    }
}
