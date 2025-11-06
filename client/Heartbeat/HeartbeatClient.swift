import Foundation

struct HeartbeatPayload: Encodable {
    let bundleID: String
    let windowTitle: String?
    let url: String?
    let activityID: String?
    let timestamp: String

    enum CodingKeys: String, CodingKey {
        case bundleID = "bundle_id"
        case windowTitle = "window_title"
        case url
        case activityID = "activity_id"
        case timestamp = "ts"
    }
}

public final class HeartbeatClient {
    private let session: URLSession
    private let baseURL: URL
    private let queue = DispatchQueue(label: "com.alfred.heartbeat")
    private let interval: TimeInterval
    private var timer: DispatchSourceTimer?
    private let encoder: JSONEncoder
    private let isoFormatter: ISO8601DateFormatter
    private let monitor = FrontAppMonitor()

    public init(baseURL: URL,
         session: URLSession = .shared,
         interval: TimeInterval = 5.0) {
        self.baseURL = baseURL
        self.session = session
        self.interval = interval

        let encoder = JSONEncoder()
        encoder.dateEncodingStrategy = .iso8601
        self.encoder = encoder
        self.isoFormatter = ISO8601DateFormatter()
    }

    public func start() {
        guard timer == nil else { return }

        let timer = DispatchSource.makeTimerSource(queue: queue)
        timer.schedule(deadline: .now() + interval, repeating: interval)
        timer.setEventHandler { [weak self] in
            self?.sendHeartbeat()
        }
        self.timer = timer
        timer.resume()

        print("🫀 Heartbeat client started (interval: \(interval)s)")
    }

    public func stop() {
        timer?.cancel()
        timer = nil
        print("🛑 Heartbeat client stopped")
    }

    private func sendHeartbeat() {
        let snapshot = monitor.snapshot()

        guard let bundleID = snapshot.bundleIdentifier else {
            print("⚠️ Heartbeat skipped: no bundle identifier")
            return
        }

        let payload = HeartbeatPayload(
            bundleID: bundleID,
            windowTitle: snapshot.tabTitle,
            url: snapshot.tabDomain,
            activityID: deriveActivityID(from: snapshot),
            timestamp: isoFormatter.string(from: Date())
        )

        guard let requestURL = URL(string: "/prod/heartbeat", relativeTo: baseURL) else {
            print("❌ Heartbeat error: invalid base URL \(baseURL)")
            return
        }

        var request = URLRequest(url: requestURL)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")

        do {
            request.httpBody = try encoder.encode(payload)
        } catch {
            print("❌ Heartbeat encoding failed: \(error)")
            return
        }

        session.dataTask(with: request) { _, response, error in
            if let error {
                print("❌ Heartbeat POST failed: \(error)")
                return
            }

            if let httpResponse = response as? HTTPURLResponse {
                print("📡 Heartbeat \(httpResponse.statusCode) bundle=\(bundleID) title=\(snapshot.tabTitle ?? "n/a") url=\(snapshot.tabDomain ?? "n/a")")
            } else {
                print("📡 Heartbeat sent (no HTTP response)")
            }
        }.resume()
    }

    private func deriveActivityID(from snapshot: FrontAppSnapshot) -> String? {
        let parts = [
            snapshot.bundleIdentifier,
            snapshot.tabTitle,
            snapshot.tabDomain
        ].compactMap { $0?.isEmpty == false ? $0 : nil }

        guard !parts.isEmpty else { return nil }
        return parts.joined(separator: "#").prefix(80).description
    }
}