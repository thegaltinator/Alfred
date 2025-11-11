import Foundation

// Use different name to avoid conflicts
public struct AlfredEnvironment {
    public static let shared = AlfredEnvironment()

    // Per architecture: NO API KEYS on client - only config endpoints
    public let cerebrasModel: String
    public let cerebrasBaseURL: String
    public let cerebrasAPIKey: String?
    public let deepInfraModel: String
    public let deepInfraBaseURL: String
    public let deepInfraAPIKey: String?
    public let cloudBaseURL: URL

    private init() {
        EnvLoader.shared.loadEnvIfNeeded()

        // Model configurations (safe to store on client)
        self.cerebrasModel = "gpt-oss-120b"
        let processEnv = ProcessInfo.processInfo.environment
        self.cerebrasBaseURL = processEnv["CEREBRAS_BASE_URL"]?.trimmingCharacters(in: .whitespacesAndNewlines).nonEmpty ?? "https://api.cerebras.ai/v1"
        self.cerebrasAPIKey = processEnv["CEREBRAS_API_KEY"]?.trimmingCharacters(in: .whitespacesAndNewlines)

        self.deepInfraModel = processEnv["DEEPINFRA_MODEL"]?.trimmingCharacters(in: .whitespacesAndNewlines).nonEmpty ?? "hexgrad/Kokoro-82M"
        self.deepInfraBaseURL = processEnv["DEEPINFRA_BASE_URL"]?.trimmingCharacters(in: .whitespacesAndNewlines).nonEmpty ?? "https://api.deepinfra.com/v1/inference"
        self.deepInfraAPIKey = processEnv["DEEPINFRA_API_KEY"]?.trimmingCharacters(in: .whitespacesAndNewlines)

        // Cloud base URL pulls from env (default to local dev)
        let defaultCloudURL = "http://127.0.0.1:8000"
        let configuredCloudURL = processEnv["ALFRED_CLOUD_BASE_URL"]?.trimmingCharacters(in: .whitespacesAndNewlines)
        let cloudURLString = (configuredCloudURL?.isEmpty == false ? configuredCloudURL! : defaultCloudURL)
        self.cloudBaseURL = URL(string: cloudURLString) ?? URL(string: defaultCloudURL)!

        print("🔧 Environment initialized (no secrets on client):")
        print("   Cerberas Model: \(cerebrasModel)")
        print("   Cerberas Base URL: \(cerebrasBaseURL)")
        if let key = cerebrasAPIKey, !key.isEmpty {
            print("   Cerberas API key loaded (\(key.prefix(6))…)")
        } else {
            print("   ⚠️ Cerberas API key missing")
        }
        print("   DeepInfra Model: \(deepInfraModel)")
        print("   DeepInfra Base URL: \(deepInfraBaseURL)")
        if let ttsKey = deepInfraAPIKey, !ttsKey.isEmpty {
            print("   DeepInfra API key loaded (\(ttsKey.prefix(6))…)")
        } else {
            print("   ⚠️ DeepInfra API key missing")
        }
        print("   Cloud Base URL: \(cloudBaseURL.absoluteString)")
    }
}
private extension String {
    var nonEmpty: String? {
        isEmpty ? nil : self
    }
}
