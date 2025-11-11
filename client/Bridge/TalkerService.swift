import Foundation
import Memory
import TTS

public actor TalkerService {
    public static let shared = TalkerService()

    private let environment = AlfredEnvironment.shared
    private lazy var cerberas = CerberasClient(model: environment.cerebrasModel,
                                              baseURL: environment.cerebrasBaseURL,
                                              apiKey: environment.cerebrasAPIKey)

    private var embedRunner: EmbedRunner?
    private var memoryBridge: MemoryBridge?
    private var socialMemorySeeded = false
    private let seedContent = "I like to listen to The Social Network soundtrack when I code."
    private let seedDefaultsKey = "com.alfred.seed.socialNetwork"

    private init() {
        if let apiKey = environment.deepInfraAPIKey, !apiKey.isEmpty {
            DeepInfraKokoro.shared.configure(apiKey: apiKey,
                                             baseURL: environment.deepInfraBaseURL,
                                             model: environment.deepInfraModel)
        }
    }

    public func sendMessage(_ userMessage: String,
                            onUpdate: @escaping (String) -> Void) async throws -> String {
        let bridge = try await prepareMemoryBridge()
        try await seedSocialMemoryIfNeeded(using: bridge)

        let relatedMemories = try await bridge.searchMemories(userMessage, limit: 3)
        logMemories(memories: relatedMemories)
        let prompt = buildPrompt(userMessage: userMessage, memories: relatedMemories)
        logPrompt(prompt)

        let reply = try await cerberas.sendMessage(prompt, onUpdate: onUpdate)
        logResponse(reply)
        await playAudio(for: reply)
        return reply
    }

    public func warmUp() async {
        do {
            _ = try await prepareMemoryBridge()
        } catch {
            print("⚠️ TalkerService warm-up failed: \(error.localizedDescription)")
        }
    }

    // MARK: - Private helpers

    private func prepareMemoryBridge() async throws -> MemoryBridge {
        if let bridge = memoryBridge {
            return bridge
        }

        let runner = try EmbedRunner()
        let bridge = try MemoryBridge(embedRunner: runner)
        self.embedRunner = runner
        self.memoryBridge = bridge
        return bridge
    }

    private func seedSocialMemoryIfNeeded(using bridge: MemoryBridge) async throws {
        if socialMemorySeeded { return }
        let defaults = UserDefaults.standard
        if defaults.bool(forKey: seedDefaultsKey) {
            socialMemorySeeded = true
            return
        }

        _ = try await bridge.addMemory(seedContent, metadata: ["seed": true])
        defaults.set(true, forKey: seedDefaultsKey)
        socialMemorySeeded = true
    }

    private func buildPrompt(userMessage: String, memories: [MemoryResult]) -> String {
        guard !memories.isEmpty else {
            return userMessage
        }

        let memoryBlock = memories.map { "- \($0.content)" }.joined(separator: "\n")
        return """
        You are Alfred, a focused personal assistant. Use the user's memories to ground your response.

        Relevant memories:
        \(memoryBlock)

        User question:
        \(userMessage)
        """
    }

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

    private func logMemories(memories: [MemoryResult]) {
        if memories.isEmpty {
            print("🧠 TalkerService: no related memories found")
        } else {
            print("🧠 TalkerService: attaching \(memories.count) memories:")
            for memory in memories {
                print("   • \(memory.content.prefix(120)) (sim: \(String(format: "%.2f", memory.similarity)))")
            }
        }
    }

    private func logPrompt(_ prompt: String) {
        let lines = prompt.split(separator: "\n")
        print("📤 TalkerService: sending prompt to Cerberas (\(prompt.count) chars, \(lines.count) lines)")
    }

    private func logResponse(_ response: String) {
        print("📥 TalkerService: Cerberas reply (\(response.count) chars)")
        print(response.prefix(200))
        if response.count > 200 {
            print("…")
        }
    }
}
