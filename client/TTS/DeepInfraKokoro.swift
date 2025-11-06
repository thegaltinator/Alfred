import Foundation
import AVFoundation

public class DeepInfraKokoro: NSObject {
    public static let shared = DeepInfraKokoro()

    private var audioPlayer: AVAudioPlayer?
    private var deepInfraAPIKey: String?
    private var deepInfraBaseURL: String = "https://api.deepinfra.com/v1/inference"
    private var deepInfraModel: String = "hexgrad/Kokoro-82M"

    private override init() {
        super.init()
    }

    /// Configure DeepInfra API credentials
    public func configure(apiKey: String, baseURL: String? = nil, model: String? = nil) {
        self.deepInfraAPIKey = apiKey
        if let baseURL = baseURL {
            self.deepInfraBaseURL = baseURL
        }
        if let model = model {
            self.deepInfraModel = model
        }
        print("🔐 DeepInfra Kokoro configured with model: \(self.deepInfraModel)")
    }

    private var isConfigured: Bool {
        return deepInfraAPIKey?.isEmpty == false
    }

    /// Synthesize and play speech using DeepInfra Kokoro
    /// - Parameter text: The text to synthesize
    /// - Parameter completion: Closure called with success status and optional error message
    public func speak(text: String, completion: @escaping (Bool, String?) -> Void) {
        print("🔊 TTS: Requesting speech for text: \"\(text)\"")

        guard isConfigured else {
            let errorMsg = "DeepInfra API key not configured"
            print("❌ TTS: \(errorMsg)")
            completion(false, errorMsg)
            return
        }

        Task {
            do {
                let audioData = try await callDeepInfraAPI(text: text)
                await MainActor.run {
                    playAudioData(audioData, completion: completion)
                }
            } catch {
                await MainActor.run {
                    let errorMsg = "TTS API call failed: \(error.localizedDescription)"
                    print("❌ TTS: \(errorMsg)")
                    completion(false, errorMsg)
                }
            }
        }
    }

    /// Call DeepInfra Kokoro API to synthesize speech
    private func callDeepInfraAPI(text: String) async throws -> Data {
        guard let apiKey = deepInfraAPIKey else {
            throw TTSError.apiKeyMissing
        }

        let urlString = "\(deepInfraBaseURL)/\(deepInfraModel)"
        guard let url = URL(string: urlString) else {
            throw TTSError.invalidURL
        }

        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.setValue("Bearer \(apiKey)", forHTTPHeaderField: "Authorization")

        let requestBody = ["text": text]
        request.httpBody = try JSONSerialization.data(withJSONObject: requestBody)

        print("🌐 TTS: Calling DeepInfra API at \(urlString)")

        let (data, response) = try await URLSession.shared.data(for: request)

        guard let httpResponse = response as? HTTPURLResponse else {
            throw TTSError.invalidResponse
        }

        print("🌐 TTS: API response status: \(httpResponse.statusCode)")

        guard httpResponse.statusCode == 200 else {
            let errorMessage = String(data: data, encoding: .utf8) ?? "Unknown error"
            throw TTSError.apiError(httpResponse.statusCode, errorMessage)
        }

        // Parse response - DeepInfra returns JSON with audio data
        guard let jsonObject = try JSONSerialization.jsonObject(with: data) as? [String: Any],
              let audioBase64 = jsonObject["audio"] as? String else {
            print("🌐 TTS: Response data: \(String(data: data, encoding: .utf8) ?? "Invalid UTF-8")")
            throw TTSError.invalidAudioData
        }

        // Remove data URL prefix if present
        let cleanBase64 = audioBase64.replacingOccurrences(of: "data:audio/wav;base64,", with: "")
        guard let audioBytes = Data(base64Encoded: cleanBase64) else {
            print("🌐 TTS: Failed to decode base64 audio data")
            throw TTSError.invalidAudioData
        }

        print("✅ TTS: Received \(audioBytes.count) bytes of audio data")
        return audioBytes
    }

    /// Play audio data using AVAudioPlayer
    private func playAudioData(_ audioData: Data, completion: @escaping (Bool, String?) -> Void) {
        do {
            // Stop any current playback
            audioPlayer?.stop()

            // Create audio player with the received data
            audioPlayer = try AVAudioPlayer(data: audioData)
            audioPlayer?.delegate = self

            guard let player = audioPlayer else {
                completion(false, "Failed to create audio player")
                return
            }

            print("🔊 TTS: Starting audio playback")
            player.play()

            // Schedule completion callback
            DispatchQueue.main.asyncAfter(deadline: .now() + player.duration + 0.5) {
                completion(true, nil)
            }

        } catch {
            let errorMsg = "Failed to play audio: \(error.localizedDescription)"
            print("❌ TTS: \(errorMsg)")
            completion(false, errorMsg)
        }
    }

    /// Stop current playback if any
    public func stop() {
        audioPlayer?.stop()
        audioPlayer = nil
        print("🔊 TTS: Playback stopped")
    }

    /// Pause current playback if any
    public func pause() {
        audioPlayer?.pause()
        print("🔊 TTS: Playback paused")
    }

    /// Resume paused playback if any
    public func resume() {
        audioPlayer?.play()
        print("🔊 TTS: Playback resumed")
    }
}

// MARK: - AVAudioPlayerDelegate
extension DeepInfraKokoro: AVAudioPlayerDelegate {
    public func audioPlayerDidFinishPlaying(_ player: AVAudioPlayer, successfully flag: Bool) {
        print("🔊 TTS: Audio playback finished (success: \(flag))")
        audioPlayer = nil
    }

    public func audioPlayerDecodeErrorDidOccur(_ player: AVAudioPlayer, error: Error?) {
        let errorMsg = "Audio decode error: \(error?.localizedDescription ?? "Unknown error")"
        print("❌ TTS: \(errorMsg)")
        audioPlayer = nil
    }
}

// MARK: - Error Types
enum TTSError: Error, LocalizedError {
    case apiKeyMissing
    case invalidURL
    case invalidResponse
    case apiError(Int, String)
    case invalidAudioData

    var errorDescription: String? {
        switch self {
        case .apiKeyMissing:
            return "DeepInfra API key not configured"
        case .invalidURL:
            return "Invalid API URL"
        case .invalidResponse:
            return "Invalid response from server"
        case .apiError(let code, let message):
            return "API error \(code): \(message)"
        case .invalidAudioData:
            return "Invalid audio data in response"
        }
    }
}