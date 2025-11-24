import Foundation
import Network

/// Manages real-time connection to the whiteboard Server-Sent Events endpoint
@MainActor
class WhiteboardClient: ObservableObject {
    private let baseURL: String
    private let userID: String
    private var task: Task<Void, Never>?
    private var urlSession: URLSession?

    @Published var messages: [WhiteboardMessage] = []
    @Published var connectionStatus: ConnectionStatus = .disconnected
    @Published var error: String?

    /// Current thread ID for conversation isolation
    private(set) var currentThreadID: UUID

    /// Initialize with a new thread ID
    init(baseURL: String, userID: String = "test-user") {
        self.baseURL = baseURL
        self.userID = userID
        self.currentThreadID = UUID()
    }

    enum ConnectionStatus {
        case connecting
        case connected
        case disconnected
        case error(String)
    }

    /// Start listening to the whiteboard stream
    func startListening() {
        guard task == nil else { return }

        connectionStatus = .connecting
        error = nil

        task = Task {
            await listenForEvents()
        }
    }

    /// Stop listening to the whiteboard stream
    func stopListening() {
        task?.cancel()
        task = nil
        urlSession?.invalidateAndCancel()
        urlSession = nil
        connectionStatus = .disconnected
    }

    private func listenForEvents() async {
        var components = URLComponents(string: "\(baseURL)/wb/stream")!
        components.queryItems = [
            URLQueryItem(name: "user_id", value: userID),
            URLQueryItem(name: "thread_id", value: currentThreadID.uuidString)
        ]

        guard let url = components.url else {
            await MainActor.run {
                self.connectionStatus = .error("Invalid URL")
                self.error = "Failed to construct whiteboard stream URL"
            }
            return
        }

        var request = URLRequest(url: url)
        request.setValue("text/event-stream", forHTTPHeaderField: "Accept")
        request.cachePolicy = .reloadIgnoringLocalCacheData
        request.timeoutInterval = 60.0

        let config = URLSessionConfiguration.default
        config.timeoutIntervalForRequest = 60.0
        config.timeoutIntervalForResource = 300.0 // 5 minutes for streaming
        urlSession = URLSession(configuration: config)

        do {
            let (bytes, response) = try await urlSession!.bytes(for: request)

            guard let httpResponse = response as? HTTPURLResponse else {
                await MainActor.run {
                    self.connectionStatus = .error("Invalid response")
                    self.error = "Server returned invalid response"
                }
                return
            }

            guard httpResponse.statusCode == 200 else {
                await MainActor.run {
                    self.connectionStatus = .error("HTTP \(httpResponse.statusCode)")
                    self.error = "Server returned HTTP \(httpResponse.statusCode)"
                }
                return
            }

            await MainActor.run {
                self.connectionStatus = .connected
                self.error = nil
            }

            // Parse SSE stream
            var currentEvent: WhiteboardMessage?
            var buffer = ""

            for try await byte in bytes {
                if Task.isCancelled { break }

                let string = String(bytes: [byte], encoding: .utf8) ?? ""
                buffer += string

                // Process complete lines
                while let newlineIndex = buffer.firstIndex(of: "\n") {
                    let line = String(buffer[..<newlineIndex])
                    buffer = String(buffer[buffer.index(after: newlineIndex)...])

                    if line.isEmpty {
                        // Empty line marks end of event
                        if let event = currentEvent {
                            await MainActor.run {
                                // Insert at beginning for newest-first order
                                self.messages.insert(event, at: 0)
                                // Keep only last 100 messages to prevent memory issues
                                if self.messages.count > 100 {
                                    self.messages.removeLast()
                                }
                            }
                            currentEvent = nil
                        }
                        continue
                    }

                    if line.hasPrefix("data: ") {
                        let data = String(line.dropFirst(6))
                        if let eventData = data.data(using: .utf8) {
                            currentEvent = try? JSONDecoder().decode(WhiteboardMessage.self, from: eventData)
                        }
                    }
                }
            }
        } catch {
            if Task.isCancelled {
                await MainActor.run {
                    self.connectionStatus = .disconnected
                }
                return
            }

            await MainActor.run {
                self.connectionStatus = .error(error.localizedDescription)
                self.error = error.localizedDescription
            }
        }
    }

    /// Append an event with the current thread ID
    func appendEvent(values: [String: Any]) async throws {
        let url = URL(string: "\(baseURL)/admin/wb/append")!
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")

        let requestBody = [
            "user_id": userID,
            "thread_id": currentThreadID.uuidString,
            "values": values
        ] as [String: Any]

        request.httpBody = try JSONSerialization.data(withJSONObject: requestBody)

        let (_, response) = try await URLSession.shared.data(for: request)

        guard let httpResponse = response as? HTTPURLResponse,
              httpResponse.statusCode == 200 else {
            throw WhiteboardError.appendFailed
        }
    }

    /// Start a new conversation with a fresh thread ID and reconnect if needed
    func startNewThread() {
        self.currentThreadID = UUID()
        if task != nil {
            stopListening()
            startListening()
        }
    }

    deinit {
        Task { @MainActor in
            stopListening()
        }
    }
}

enum WhiteboardError: Error {
    case appendFailed
    case invalidResponse

    var localizedDescription: String {
        switch self {
        case .appendFailed:
            return "Failed to append event to whiteboard"
        case .invalidResponse:
            return "Invalid response from whiteboard server"
        }
    }
}

/// Represents a whiteboard message from the server
struct WhiteboardMessage: Codable, Identifiable {
    let id: String
    let stream: String
    let userID: String
    let threadID: String?
    let values: [String: AnyCodable]
    let timestamp: String

    // Computed properties for display
    var displayTitle: String {
        values["type"]?.stringValue ?? "Message"
    }

    var displayContent: String {
        if let content = values["content"]?.stringValue {
            return content
        }
        if let summary = values["summary"]?.stringValue {
            return summary
        }
        if let decision = values["decision"]?.stringValue {
            return decision
        }
        return values.description
    }

    var formattedTime: String {
        // Parse ISO8601 timestamp and format for display
        let formatter = ISO8601DateFormatter()
        if let date = formatter.date(from: timestamp) {
            let displayFormatter = DateFormatter()
            displayFormatter.dateStyle = .none
            displayFormatter.timeStyle = .short
            return displayFormatter.string(from: date)
        }
        return timestamp
    }

    /// Check if this message belongs to the current thread
    func belongsToThread(_ threadID: UUID) -> Bool {
        return self.threadID == threadID.uuidString
    }
}

/// Helper type to decode Any values from JSON
enum AnyCodable: Codable {
    case string(String)
    case int(Int)
    case double(Double)
    case bool(Bool)
    case array([AnyCodable])
    case dictionary([String: AnyCodable])
    case null

    var stringValue: String? {
        if case .string(let value) = self { return value }
        return nil
    }

    var description: String {
        switch self {
        case .string(let value): return value
        case .int(let value): return "\(value)"
        case .double(let value): return "\(value)"
        case .bool(let value): return "\(value)"
        case .array(let array): return "[\(array.count) items]"
        case .dictionary(let dict): return "{\(dict.count) keys}"
        case .null: return "null"
        }
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.singleValueContainer()

        if container.decodeNil() {
            self = .null
        } else if let stringValue = try? container.decode(String.self) {
            self = .string(stringValue)
        } else if let intValue = try? container.decode(Int.self) {
            self = .int(intValue)
        } else if let doubleValue = try? container.decode(Double.self) {
            self = .double(doubleValue)
        } else if let boolValue = try? container.decode(Bool.self) {
            self = .bool(boolValue)
        } else if let arrayValue = try? container.decode([AnyCodable].self) {
            self = .array(arrayValue)
        } else if let dictValue = try? container.decode([String: AnyCodable].self) {
            self = .dictionary(dictValue)
        } else {
            throw DecodingError.dataCorrupted(
                DecodingError.Context(codingPath: decoder.codingPath, debugDescription: "Unsupported type")
            )
        }
    }

    func encode(to encoder: Encoder) throws {
        var container = encoder.singleValueContainer()

        switch self {
        case .null:
            try container.encodeNil()
        case .string(let value):
            try container.encode(value)
        case .int(let value):
            try container.encode(value)
        case .double(let value):
            try container.encode(value)
        case .bool(let value):
            try container.encode(value)
        case .array(let value):
            try container.encode(value)
        case .dictionary(let value):
            try container.encode(value)
        }
    }
}
