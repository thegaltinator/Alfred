import Foundation
import TTS

public actor TalkerService {
    public static let shared = TalkerService()

    private let environment = AlfredEnvironment.shared
    private let helperClient = PythonHelperClient()

    private init() {
        if let apiKey = environment.deepInfraAPIKey, !apiKey.isEmpty {
            DeepInfraKokoro.shared.configure(apiKey: apiKey,
                                             baseURL: environment.deepInfraBaseURL,
                                             model: environment.deepInfraModel)
        }
    }

    public func sendMessage(_ userMessage: String,
                            onUpdate: @escaping (String) -> Void) async throws -> String {
        let sessionID = UUID().uuidString
        logPrompt(userMessage)
        let response = try await helperClient.chat(sessionID: sessionID,
                                                   userText: userMessage,
                                                   options: .init())
        let reply = response.assistantText
        onUpdate(reply)
        logResponse(reply)
        await playAudio(for: reply)
        return reply
    }

    public func warmUp() async {
        await helperClient.warmUp()
        print("⚙️ TalkerService warm-up (Python helper ready)")
    }

    // MARK: - Private helpers

    private func playAudio(for text: String) async {
        guard environment.deepInfraAPIKey?.isEmpty == false else { return }
        print("🔊 TalkerService: sending reply to DeepInfra (\(text.count) chars)")
        DeepInfraKokoro.shared.speak(text: text) { success, error in
            if let error {
                print("❌ TTS playback failed: \(error)")
            } else {
                print("🔊 TTS playback success: \(success)")
            }
        }
    }

    private func logPrompt(_ prompt: String) {
        let lines = prompt.split(separator: "\n")
        print("📤 TalkerService: sending turn to Python helper (\(prompt.count) chars, \(lines.count) lines)")
    }

    private func logResponse(_ response: String) {
        print("📥 TalkerService: helper/Cerberas reply (\(response.count) chars)")
        print(response.prefix(200))
        if response.count > 200 {
            print("…")
        }
    }
}
