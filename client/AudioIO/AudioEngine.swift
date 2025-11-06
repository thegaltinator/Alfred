import Foundation
import AVFoundation

class AudioEngine: NSObject {
    static let shared = AudioEngine()

    private var audioEngine: AVAudioEngine?
    private var playerNode: AVAudioPlayerNode?

    private override init() {
        super.init()
        setupAudioEngine()
    }

    private func setupAudioEngine() {
        do {
            // Setup audio engine (macOS doesn't use AVAudioSession)
            audioEngine = AVAudioEngine()
            playerNode = AVAudioPlayerNode()

            guard let audioEngine = audioEngine,
                  let playerNode = playerNode else {
                print("❌ Failed to initialize audio engine components")
                return
            }

            // Connect player node to main mixer
            audioEngine.attach(playerNode)
            audioEngine.connect(playerNode, to: audioEngine.mainMixerNode, format: nil)

            // Start the engine
            try audioEngine.start()
            print("🔊 Audio engine started successfully")

        } catch {
            print("❌ Failed to setup audio engine: \(error.localizedDescription)")
        }
    }

    /// Play audio data from Data
    func playAudioData(_ audioData: Data, completion: @escaping (Bool, String?) -> Void) {
        // Write audio data to temporary file
        let tempURL = FileManager.default.temporaryDirectory.appendingPathComponent("temp_audio_\(UUID().uuidString).wav")

        do {
            try audioData.write(to: tempURL)
            playAudioFile(at: tempURL, completion: completion)

            // Clean up temp file after playback
            DispatchQueue.main.asyncAfter(deadline: .now() + 10) {
                try? FileManager.default.removeItem(at: tempURL)
            }

        } catch {
            print("❌ Failed to write audio data to temp file: \(error.localizedDescription)")
            completion(false, error.localizedDescription)
        }
    }

    /// Play audio from a file URL
    func playAudioFile(at url: URL, completion: @escaping (Bool, String?) -> Void) {
        guard let playerNode = playerNode else {
            completion(false, "Audio engine not initialized")
            return
        }

        do {
            let audioFile = try AVAudioFile(forReading: url)

            playerNode.scheduleFile(audioFile, at: nil) {
                DispatchQueue.main.async {
                    completion(true, nil)
                }
            }

            if !playerNode.isPlaying {
                playerNode.play()
            }

            print("🔊 Playing audio file: \(url.lastPathComponent)")

        } catch {
            print("❌ Failed to play audio file: \(error.localizedDescription)")
            completion(false, error.localizedDescription)
        }
    }

    /// Stop current playback
    func stop() {
        playerNode?.stop()
        print("🔊 Audio playback stopped")
    }

    /// Pause current playback
    func pause() {
        playerNode?.pause()
        print("🔊 Audio playback paused")
    }

    /// Resume paused playback
    func resume() {
        playerNode?.play()
        print("🔊 Audio playback resumed")
    }

    /// Check if currently playing
    var isPlaying: Bool {
        return playerNode?.isPlaying ?? false
    }
}