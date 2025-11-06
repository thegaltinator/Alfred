import Foundation

// Use different name to avoid conflicts
public struct AlfredEnvironment {
    public static let shared = AlfredEnvironment()

    // Per architecture: NO API KEYS on client - only config endpoints
    public let cerebrasModel: String
    public let cerebrasBaseURL: String
    public let deepInfraModel: String
    public let deepInfraBaseURL: String
    public let cloudBaseURL: URL

    private init() {
        // Model configurations (safe to store on client)
        self.cerebrasModel = "gpt-oss-120b"
        self.cerebrasBaseURL = "https://api.cerebras.ai/v1"

        self.deepInfraModel = "hexgrad/Kokoro-82M"
        self.deepInfraBaseURL = "https://api.deepinfra.com/v1/inference"

        // Cloud base URL pulls from env (default to local dev)
        let processEnv = ProcessInfo.processInfo.environment
        let defaultCloudURL = "http://127.0.0.1:8000"
        let configuredCloudURL = processEnv["ALFRED_CLOUD_BASE_URL"]?.trimmingCharacters(in: .whitespacesAndNewlines)
        let cloudURLString = (configuredCloudURL?.isEmpty == false ? configuredCloudURL! : defaultCloudURL)
        self.cloudBaseURL = URL(string: cloudURLString) ?? URL(string: defaultCloudURL)!

        print("🔧 Environment initialized (no secrets on client):")
        print("   Cerberas Model: \(cerebrasModel)")
        print("   Cerberas Base URL: \(cerebrasBaseURL)")
        print("   DeepInfra Model: \(deepInfraModel)")
        print("   DeepInfra Base URL: \(deepInfraBaseURL)")
        print("   Cloud Base URL: \(cloudBaseURL.absoluteString)")
    }
}