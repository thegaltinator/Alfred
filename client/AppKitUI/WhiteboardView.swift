import SwiftUI
import Foundation
import Bridge

// MARK: - Whiteboard Models

/// Represents a whiteboard message from the server
struct WhiteboardMessage: Codable, Identifiable {
    let ID: String
    let Stream: String
    let UserID: String
    let Values: [String: AnyCodable]

    // Conform to Identifiable protocol
    var id: String { self.ID }

    // Computed properties for display (mapping server structure to display logic)
    var displayTitle: String {
        Values["type"]?.stringValue ?? "Message"
    }

    var displayContent: String {
        if let content = Values["content"]?.stringValue {
            return content
        }
        if let summary = Values["summary"]?.stringValue {
            return summary
        }
        if let decision = Values["decision"]?.stringValue {
            return decision
        }
        return Values.description
    }

    var formattedTime: String {
        // Extract timestamp from Values["ts"] and format for display
        if let timestamp = Values["ts"]?.stringValue {
            let formatter = ISO8601DateFormatter()
            if let date = formatter.date(from: timestamp) {
                let displayFormatter = DateFormatter()
                displayFormatter.dateStyle = .none
                displayFormatter.timeStyle = .short
                return displayFormatter.string(from: date)
            }
            return timestamp
        }
        return self.ID // fallback to ID if no timestamp
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

    enum ConnectionStatus {
        case connecting
        case connected
        case disconnected
        case error(String)
    }

    init(baseURL: String, userID: String = "test-user") {
        self.baseURL = baseURL
        self.userID = userID
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
        print("🔗 WhiteboardClient attempting to connect to: \(baseURL)")
        var components = URLComponents(string: "\(baseURL)/wb/stream")
        components?.queryItems = [URLQueryItem(name: "user_id", value: userID)]

        print("🔗 Full URL being requested: \(components?.url?.absoluteString ?? "invalid URL")")
        guard let url = components?.url else {
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
            var currentEventID: String?
            var currentEventData: String?
            var buffer = ""

            for try await line in bytes {
                if Task.isCancelled { break }

                let string = String(bytes: [line], encoding: .utf8) ?? ""
                buffer += string

                // Process complete lines
                while let newlineIndex = buffer.firstIndex(of: "\n") {
                    let line = String(buffer[..<newlineIndex])
                    buffer = String(buffer[buffer.index(after: newlineIndex)...])

                    print("🔍 SSE line: \(line)") // Debug log each line

                    if line.isEmpty {
                        // Empty line marks end of event
                        print("🚨 SSE: End of event detected") // Debug log
                        if let eventData = currentEventData, !eventData.isEmpty {
                            print("📥 SSE: Processing event data: \(eventData)") // Debug log
                            if let eventData = eventData.data(using: .utf8) {
                                do {
                                    let event = try JSONDecoder().decode(WhiteboardMessage.self, from: eventData)
                                    print("✅ SSE: Successfully decoded event: \(event.displayTitle) - \(event.displayContent)") // Debug log
                                    await MainActor.run {
                                        // Insert at beginning for newest-first order
                                        self.messages.insert(event, at: 0)
                                        print("💾 SSE: Added message to UI. Total count: \(self.messages.count)") // Debug log
                                        // Keep only last 100 messages to prevent memory issues
                                        if self.messages.count > 100 {
                                            self.messages.removeLast()
                                        }
                                    }
                                } catch {
                                    print("❌ SSE: JSON decode error: \(error)") // Debug log
                                    print("❌ SSE: Failed data was: \(eventData)") // Debug log
                                }
                            }
                        }
                        // Reset for next event
                        currentEventID = nil
                        currentEventData = nil
                        continue
                    }

                    if line.hasPrefix("id: ") {
                        currentEventID = String(line.dropFirst(4))
                        print("🏷️ SSE: Found event ID: \(currentEventID!)") // Debug log
                    } else if line.hasPrefix("data: ") {
                        currentEventData = String(line.dropFirst(6))
                        if let dataPreview = currentEventData?.prefix(100) {
                            print("📦 SSE: Found data: \(String(dataPreview))...") // Debug log
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

    // deinit disabled to avoid Swift 6 issues
    // deinit {
    //     Task { @MainActor in
    //         stopListening()
    //     }
    // }
}

struct WhiteboardView: View {
    @StateObject private var whiteboardClient: WhiteboardClient
    @Environment(\.colorScheme) private var colorScheme
    let onClose: () -> Void

    init(onClose: @escaping () -> Void) {
        self.onClose = onClose
        let environment = AlfredEnvironment.shared
        self._whiteboardClient = StateObject(wrappedValue: WhiteboardClient(
            baseURL: environment.cloudBaseURL.absoluteString
        ))
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            // Header
            headerView

            Divider()

            // Connection status
            connectionStatusView

            Divider()

            // Messages list
            messagesView

            Spacer()
        }
        .frame(width: 400, height: 500)
        .onAppear {
            whiteboardClient.startListening()
        }
        .onDisappear {
            whiteboardClient.stopListening()
        }
    }

    private var headerView: some View {
        HStack {
            Text("Whiteboard")
                .font(.title2)
                .fontWeight(.semibold)

            Spacer()

            Button(action: onClose) {
                Image(systemName: "xmark.circle.fill")
                    .font(.title2)
                    .foregroundColor(.secondary)
            }
            .buttonStyle(BorderlessButtonStyle())
            .help("Close")
        }
        .padding()
    }

    private var connectionStatusView: some View {
        HStack {
            connectionStatusIndicator

            Text(connectionStatusText)
                .font(.caption)
                .foregroundColor(statusTextColor)

            Spacer()

            if whiteboardClient.messages.count > 0 {
                Text("\(whiteboardClient.messages.count) messages")
                    .font(.caption)
                    .foregroundColor(.secondary)
            }
        }
        .padding(.horizontal)
        .padding(.vertical, 8)
        .background(statusBackgroundColor.opacity(0.1))
    }

    @ViewBuilder
    private var connectionStatusIndicator: some View {
        switch whiteboardClient.connectionStatus {
        case .connecting:
            ProgressView()
                .scaleEffect(0.7)
        case .connected:
            Image(systemName: "circle.fill")
                .foregroundColor(.green)
                .font(.caption)
        case .disconnected:
            Image(systemName: "circle.fill")
                .foregroundColor(.gray)
                .font(.caption)
        case .error:
            Image(systemName: "exclamationmark.triangle.fill")
                .foregroundColor(.red)
                .font(.caption)
        }
    }

    private var connectionStatusText: String {
        switch whiteboardClient.connectionStatus {
        case .connecting:
            return "Connecting..."
        case .connected:
            return "Connected"
        case .disconnected:
            return "Disconnected"
        case .error(let message):
            return "Error: \(message)"
        }
    }

    private var statusTextColor: Color {
        switch whiteboardClient.connectionStatus {
        case .connecting, .connected, .disconnected:
            return .primary
        case .error:
            return .red
        }
    }

    private var statusBackgroundColor: Color {
        switch whiteboardClient.connectionStatus {
        case .connected:
            return .green
        case .error:
            return .red
        default:
            return .gray
        }
    }

    @ViewBuilder
    private var messagesView: some View {
        if whiteboardClient.messages.isEmpty {
            emptyStateView
        } else {
            ScrollView {
                LazyVStack(spacing: 0) {
                    ForEach(whiteboardClient.messages) { message in
                        messageRow(message)

                        // Add separator between messages
                        if message.id != whiteboardClient.messages.last?.id {
                            Divider()
                                .padding(.leading, 12)
                        }
                    }
                }
            }
        }
    }

    private var emptyStateView: some View {
        VStack(spacing: 16) {
            Image(systemName: "tray")
                .font(.system(size: 48))
                .foregroundColor(.secondary)

            VStack(spacing: 8) {
                Text("No messages yet")
                    .font(.headline)
                    .foregroundColor(.primary)

                Text("Whiteboard messages from subagents will appear here")
                    .font(.body)
                    .foregroundColor(.secondary)
                    .multilineTextAlignment(.center)
            }

            if let error = whiteboardClient.error {
                Text("Error: \(error)")
                    .font(.caption)
                    .foregroundColor(.red)
                    .padding()
                    .background(Color.red.opacity(0.1))
                    .cornerRadius(8)
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .padding()
    }

    private func messageRow(_ message: WhiteboardMessage) -> some View {
        HStack(alignment: .top, spacing: 12) {
            // Message type icon
            messageIcon(for: message.displayTitle)
                .frame(width: 24, height: 24)

            VStack(alignment: .leading, spacing: 4) {
                // Message header
                HStack {
                    Text(message.displayTitle)
                        .font(.caption)
                        .fontWeight(.medium)
                        .foregroundColor(.primary)

                    Spacer()

                    Text(message.formattedTime)
                        .font(.caption2)
                        .foregroundColor(.secondary)
                }

                // Message content
                Text(message.displayContent)
                    .font(.body)
                    .foregroundColor(.primary)
                    .fixedSize(horizontal: false, vertical: true)
                    .lineLimit(nil)
                    .multilineTextAlignment(.leading)
            }
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 8)
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    @ViewBuilder
    private func messageIcon(for title: String) -> some View {
        let (iconName, iconColor) = iconForTitle(title)

        Image(systemName: iconName)
            .font(.caption)
            .foregroundColor(iconColor)
            .frame(width: 24, height: 24)
            .background(iconColor.opacity(0.2))
            .clipShape(Circle())
    }

    private func iconForTitle(_ title: String) -> (String, Color) {
        switch title.lowercased() {
        case let t where t.contains("calendar") || t.contains("planner"):
            return ("calendar", .blue)
        case let t where t.contains("productivity") || t.contains("underrun") || t.contains("overrun"):
            return ("chart.line.uptrend.xyaxis", .orange)
        case let t where t.contains("email") || t.contains("triage"):
            return ("envelope", .green)
        case let t where t.contains("nudge"):
            return ("bell", .purple)
        default:
            return ("text.bubble", .gray)
        }
    }
}

// MARK: - Preview
struct WhiteboardView_Previews: PreviewProvider {
    static var previews: some View {
        Group {
            WhiteboardView(onClose: {})
                .preferredColorScheme(.light)

            WhiteboardView(onClose: {})
                .preferredColorScheme(.dark)
        }
    }
}