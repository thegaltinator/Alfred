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
        helperPath = PythonHelperClient.resolveHelperPath(explicit: scriptPath).path
    }

    func warmUp() async {
        try? ensureProcess()
    }

    func chat(sessionID: String,
              userText: String,
              options: Options = Options()) async throws -> ResponseBody {
        print("🔍 PythonHelperClient.chat starting")
        print("   - sessionID: \(sessionID)")
        print("   - userText: '\(userText)'")
        print("   - options: topK=\(options.topK), minScore=\(options.minScore), temperature=\(options.temperature), stream=\(options.stream)")

        do {
            try ensureProcess()
            // Double check process is still running
            if let p = process, !p.isRunning {
                print("❌ Process exited immediately after ensureProcess")
                resetProcess()
                throw PythonHelperError.processExited
            }
            print("✅ Process ensured successfully")
        } catch {
            print("❌ ensureProcess failed: \(error)")
            throw error
        }

        guard let stdinHandle = stdinPipe?.fileHandleForWriting else {
            let error = PythonHelperError.processUnavailable
            print("❌ stdin handle not available: \(error)")
            throw error
        }
        print("✅ stdin handle available")

        let body = RequestBody(sessionID: sessionID, userText: userText, opts: options)
        print("📤 Creating JSON request...")

        do {
            // Validate process health before writing
            guard let process = self.process, process.isRunning else {
                let error = PythonHelperError.processExited
                print("❌ Process not running before write: \(error)")
                resetProcess()
                throw error
            }

            // Validate file handle before operations
            guard stdinHandle.fileDescriptor != -1 else {
                let error = PythonHelperError.processUnavailable
                print("❌ Invalid stdin file descriptor: \(error)")
                resetProcess()
                throw error
            }

            let data = try encoder.encode(body)
            print("✅ JSON encoded successfully (\(data.count) bytes)")

            // Safe write operations with exception handling
            do {
                if #available(macOS 10.15.4, *) {
                    try stdinHandle.write(contentsOf: data)
                    try stdinHandle.write(contentsOf: Data([0x0A]))
                } else {
                    stdinHandle.write(data)
                    stdinHandle.write(Data([0x0A]))
                }

                // Pipe writes don't need explicit synchronize; just validate still open/running
                if stdinHandle.fileDescriptor != -1 && process.isRunning {
                    print("✅ Data written to stdin")
                } else {
                    let error = PythonHelperError.processExited
                    print("❌ Process or file handle became invalid during write: \(error)")
                    resetProcess()
                    throw error
                }
            } catch {
                print("❌ Write failed: \(error)")
                resetProcess()
                throw PythonHelperError.processExited
            }
        } catch {
            print("❌ JSON encoding or write failed: \(error)")
            resetProcess()
            throw error
        }

        print("📥 Reading response from helper...")
        do {
            let line = try readLineFromHelper()
            print("✅ Read line: '\(String(line.prefix(100)))'")

            guard let jsonData = line.data(using: .utf8) else {
                let error = PythonHelperError.invalidPayload(line)
                print("❌ Invalid UTF-8 data: \(error)")
                throw error
            }
            print("✅ Converted to UTF-8 data (\(jsonData.count) bytes)")

            if let envelope = try? decoder.decode(ErrorEnvelope.self, from: jsonData) {
                let error = PythonHelperError.helperError(envelope.error)
                print("❌ Helper returned error: \(error)")
                throw error
            }

            do {
                let response = try decoder.decode(ResponseBody.self, from: jsonData)
                print("✅ Successfully decoded response")
                print("   - assistantText: '\(String(response.assistantText.prefix(100)))'")
                print("   - usedMemory: \(response.usedMemory.count) items")
                print("   - latencyMs: \(response.latencyMs)")
                print("   - tokenUsage: \(response.tokenUsage.prompt) + \(response.tokenUsage.completion)")
                return response
            } catch {
                print("❌ Failed to decode ResponseBody: \(error)")
                print("   JSON data: \(String(data: jsonData, encoding: .utf8) ?? "invalid utf8")")
                throw error
            }
        } catch {
            print("❌ readLineFromHelper failed: \(error)")
            throw error
        }
    }

    private func ensureProcess() throws {
        if let process, process.isRunning {
            return
        }

        let fm = FileManager.default
        let process = Process()

        // Find the virtual environment Python interpreter
        let scriptURL = URL(fileURLWithPath: helperPath)
        guard fm.fileExists(atPath: scriptURL.path) else {
            let error = PythonHelperError.missingHelperScript(scriptURL.path)
            print("❌ Helper script not found at \(scriptURL.path)")
            throw error
        }
        let pythonHelperDir = scriptURL.deletingLastPathComponent()
        let venvPythonPath = pythonHelperDir.appendingPathComponent("venv/bin/python3").path
        let envPath = "/usr/bin/env"

        // Check if virtual environment exists, otherwise use system python3
        if fm.isExecutableFile(atPath: venvPythonPath) {
            process.executableURL = URL(fileURLWithPath: venvPythonPath)
            process.arguments = [helperPath]
            print("🐍 Using venv python at \(venvPythonPath)")
        } else if fm.isExecutableFile(atPath: envPath) {
            process.executableURL = URL(fileURLWithPath: envPath)
            process.arguments = ["python3", helperPath]
            print("🐍 Using /usr/bin/env python3")
        } else {
            let error = PythonHelperError.processUnavailable
            print("❌ No python executable found (checked \(venvPythonPath) and \(envPath))")
            throw error
        }

        process.currentDirectoryURL = pythonHelperDir
        print("📁 Helper cwd: \(pythonHelperDir.path)")
        print("📜 Helper script: \(scriptURL.path)")
        print("🚀 Launch args: \(process.arguments ?? [])")

        let stdinPipe = Pipe()
        let stdoutPipe = Pipe()

        process.standardInput = stdinPipe
        process.standardOutput = stdoutPipe
        process.standardError = FileHandle.standardError

        do {
            try process.run()
        } catch {
            print("❌ Failed to start helper process: \(error)")
            resetProcess()
            throw error
        }
        self.process = process
        self.stdinPipe = stdinPipe
        self.stdoutPipe = stdoutPipe
    }

    private func readLineFromHelper() throws -> String {
        print("🔍 readLineFromHelper starting")

        // Validate process health before reading
        guard let process = self.process, process.isRunning else {
            let error = PythonHelperError.processExited
            print("❌ Process not running before read: \(error)")
            resetProcess()
            throw error
        }

        guard let stdoutHandle = stdoutPipe?.fileHandleForReading else {
            let error = PythonHelperError.processUnavailable
            print("❌ stdout handle not available: \(error)")
            resetProcess()
            throw error
        }

        // Validate file descriptor
        guard stdoutHandle.fileDescriptor != -1 else {
            let error = PythonHelperError.processUnavailable
            print("❌ Invalid stdout file descriptor: \(error)")
            resetProcess()
            throw error
        }

        print("✅ stdout handle available")

        var buffer = Data()
        var byteCount = 0
        while true {
            // Additional process validation in each iteration
            guard process.isRunning && stdoutHandle.fileDescriptor != -1 else {
                print("❌ Process or file handle became invalid during read")
                print("   Buffer so far: \(String(data: buffer, encoding: .utf8) ?? "non-utf8")")
                resetProcess()
                throw PythonHelperError.processExited
            }

            byteCount += 1

            // Safe read operation with exception handling
            let chunk: Data
            do {
                if #available(macOS 10.15.4, *) {
                    if let data = try stdoutHandle.read(upToCount: 1) {
                        chunk = data
                    } else {
                        chunk = Data() // EOF treated as empty to trigger exit check below
                    }
                } else {
                    chunk = stdoutHandle.readData(ofLength: 1)
                }
            } catch {
                print("❌ Read failed: \(error)")
                print("   Buffer so far: \(String(data: buffer, encoding: .utf8) ?? "non-utf8")")
                resetProcess()
                throw PythonHelperError.processExited
            }

            if chunk.isEmpty {
                print("❌ Process exited - no more data available after \(byteCount) bytes")
                print("   Buffer so far: \(String(data: buffer, encoding: .utf8) ?? "non-utf8")")
                resetProcess()
                throw PythonHelperError.processExited
            }

            if chunk[0] == 0x0A {
                print("✅ Found newline after \(byteCount) bytes")
                break
            }

            buffer.append(chunk)

            // Prevent infinite loops
            if byteCount > 100000 {
                print("❌ Too much data read (>100KB), possible infinite loop")
                print("   Buffer so far: \(String(data: buffer, encoding: .utf8) ?? "non-utf8")")
                throw PythonHelperError.invalidPayload("excessive data")
            }
        }

        guard let line = String(data: buffer, encoding: .utf8) else {
            print("❌ Buffer contains non-UTF8 data")
            print("   Raw bytes: \(buffer.map { String(format: "%02x", $0) }.joined())")
            throw PythonHelperError.invalidPayload("non-utf8")
        }

        print("✅ Successfully read line: '\(String(line.prefix(200)))'")
        return line
    }

    private func resetProcess() {
        process = nil
        stdinPipe = nil
        stdoutPipe = nil
    }

    private static func resolveHelperPath(explicit: String?) -> URL {
        let fm = FileManager.default
        var candidates: [URL] = []

        if let envPath = ProcessInfo.processInfo.environment["PY_HELPER_PATH"],
           !envPath.isEmpty {
            candidates.append(URL(fileURLWithPath: envPath))
        }

        if let explicit, !explicit.isEmpty {
            candidates.append(URL(fileURLWithPath: explicit))
        }

        // Current working directory (terminal runs)
        let cwd = URL(fileURLWithPath: fm.currentDirectoryPath)
        candidates.append(cwd.appendingPathComponent("python_helper/app.py"))

        // Bundle locations (when launched as .app)
        if let execURL = Bundle.main.executableURL {
            let execDir = execURL.deletingLastPathComponent()
            candidates.append(execDir.appendingPathComponent("python_helper/app.py"))
            candidates.append(execDir.deletingLastPathComponent().appendingPathComponent("Resources/python_helper/app.py"))
            candidates.append(execDir.deletingLastPathComponent().appendingPathComponent("python_helper/app.py"))
        }

        if let resourceURL = Bundle.main.resourceURL {
            candidates.append(resourceURL.appendingPathComponent("python_helper/app.py"))
        }

        // Fallback to source tree heuristic using #file in DerivedData
        let thisFile = URL(fileURLWithPath: #file)
        let repoRoot = thisFile
            .deletingLastPathComponent() // Bridge
            .deletingLastPathComponent() // client
            .deletingLastPathComponent() // repository root
        candidates.append(repoRoot.appendingPathComponent("python_helper/app.py"))

        if let found = candidates.first(where: { fm.fileExists(atPath: $0.path) }) {
            print("✅ Python helper located at \(found.path)")
            return found
        }

        print("❌ Could not locate python_helper/app.py. Tried:")
        candidates.forEach { print("   - \($0.path)") }
        // Return the last candidate so errors include a path
        return candidates.last ?? URL(fileURLWithPath: "python_helper/app.py")
    }

    enum PythonHelperError: Error, LocalizedError {
        case processUnavailable
        case processExited
        case invalidPayload(String)
        case helperError(String)
        case missingHelperScript(String)

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
            case .missingHelperScript(let path):
                return "Python helper script missing at path: \(path)"
            }
        }
    }
}
