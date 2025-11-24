import Foundation
import TTS

public actor TalkerService {
    public static let shared = TalkerService()

    private let environment = AlfredEnvironment.shared
    private let helperClient = PythonHelperClient()
    private let cerberasClient: CerberasClient?
    private let whiteboardClient: WhiteboardClient

    private init() {
        cerberasClient = CerberasClient(model: environment.cerebrasModel,
                                        baseURL: environment.cerebrasBaseURL,
                                        apiKey: environment.cerebrasAPIKey)
        whiteboardClient = WhiteboardClient(baseURL: environment.cloudBaseURL)
        if let apiKey = environment.deepInfraAPIKey, !apiKey.isEmpty {
            DeepInfraKokoro.shared.configure(apiKey: apiKey,
                                             baseURL: environment.deepInfraBaseURL,
                                             model: environment.deepInfraModel)
        }
    }

    public func sendMessage(_ userMessage: String,
                            onUpdate: @escaping (String) -> Void) async throws -> String {
        return try await sendMessageWithThread(userMessage, threadID: whiteboardClient.currentThreadID, onUpdate: onUpdate)
    }

    public func sendMessageWithThread(_ userMessage: String,
                                     threadID: UUID,
                                     onUpdate: @escaping (String) -> Void) async throws -> String {
        print("🚀 TalkerService.sendMessageWithThread starting")
        print("   - userMessage: '\(userMessage)'")
        print("   - threadID: '\(threadID.uuidString)'")

        let sessionID = UUID().uuidString
        logPrompt(userMessage, threadID: threadID)

        // Log user message to whiteboard with thread ID
        do {
            try await whiteboardClient.appendEvent(values: [
                "type": "talker.user_message",
                "content": userMessage,
                "session_id": sessionID
            ])
            print("✅ User message logged to whiteboard with thread ID")
        } catch {
            print("⚠️ Failed to log user message to whiteboard: \(error)")
        }

        do {
            print("📞 Calling python helper (memory + Cerberas)...")
            let response = try await helperClient.chat(sessionID: sessionID,
                                                       userText: userMessage,
                                                       options: .init())
            print("✅ helperClient.chat succeeded")

            let reply = response.assistantText
            print("📨 Got reply: '\(String(reply.prefix(100)))'")

            onUpdate(reply)
            logResponse(reply, threadID: threadID)
            await playAudio(for: reply)

            // Log assistant reply to whiteboard with thread ID
            do {
                try await whiteboardClient.appendEvent(values: [
                    "type": "talker.assistant_reply",
                    "content": reply,
                    "session_id": sessionID
                ])
                print("✅ Assistant reply logged to whiteboard with thread ID")
            } catch {
                print("⚠️ Failed to log assistant reply to whiteboard: \(error)")
            }

            return reply
        } catch {
            print("❌ Python helper failed: \(error)")
            print("   - Error type: \(type(of: error))")
            print("   - Error localized: \(error.localizedDescription)")
        }

        do {
            if let cerberasClient {
                print("📞 Falling back to Cerberas (streaming, no memory)...")
                let reply = try await cerberasClient.streamMessage(userMessage) { partial in
                    print("📨 Cerberas partial: '\(String(partial.suffix(40)))'")
                    onUpdate(partial)
                }
                print("✅ Cerberas stream succeeded (fallback)")
                logResponse(reply)
                await playAudio(for: reply)
                return reply
            }
        } catch {
            print("❌ Cerberas fallback failed: \(error)")
        }

        let error = PythonHelperClient.PythonHelperError.processUnavailable
        print("❌ TalkerService.sendMessage failed: \(error.localizedDescription)")
        throw error
    }

    public func warmUp() async {
        await helperClient.warmUp()
        print("⚙️ TalkerService warm-up (Python helper ready)")
    }

    /// Start a new conversation thread
    public func startNewThread() {
        whiteboardClient.startNewThread()
        print("🧵 TalkerService: Started new conversation thread")
    }

    /// Get the current thread ID
    public var currentThreadID: UUID {
        return whiteboardClient.currentThreadID
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

    private func logPrompt(_ prompt: String, threadID: UUID) {
        let lines = prompt.split(separator: "\n")
        print("📤 TalkerService: sending turn to Python helper (\(prompt.count) chars, \(lines.count) lines, thread: \(threadID.uuidString.prefix(8)))")
    }

    private func logResponse(_ response: String, threadID: UUID) {
        print("📥 TalkerService: helper/Cerberas reply (\(response.count) chars, thread: \(threadID.uuidString.prefix(8)))")
        print(response.prefix(200))
        if response.count > 200 {
            print("…")
        }
    }
}
