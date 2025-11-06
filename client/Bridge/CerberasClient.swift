import Foundation

struct CerberasClient {
    let model: String
    let baseURL: String

    init(model: String, baseURL: String = "https://api.cerebras.ai/v1") {
        self.model = model
        self.baseURL = baseURL
    }

    func sendMessage(_ message: String, onUpdate: @escaping (String) -> Void) async throws -> String {
        // Per architecture: Call through cloud server, not directly to Cerberas
        // Cloud server handles API keys and acts as proxy
        let environment = AlfredEnvironment.shared
        let url = URL(string: "\(environment.cloudBaseURL.absoluteString)/api/cerberas/chat")!

        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")

        let requestBody = [
            "message": message,
            "model": model
        ]

        request.httpBody = try JSONSerialization.data(withJSONObject: requestBody)

        // TODO: Add auth token when G-07 Clerk auth is implemented

        let (data, response) = try await URLSession.shared.data(for: request)

        guard let httpResponse = response as? HTTPURLResponse else {
            throw CerberasError.invalidResponse
        }

        guard httpResponse.statusCode == 200 else {
            throw CerberasError.apiError(httpResponse.statusCode, "HTTP \(httpResponse.statusCode)")
        }

        guard let json = try JSONSerialization.jsonObject(with: data) as? [String: Any],
              let responseText = json["response"] as? String else {
            throw CerberasError.invalidResponse
        }

        return responseText
    }

    enum CerberasError: Error, LocalizedError {
        case missingAPIKey
        case invalidResponse
        case apiError(Int, String)
        case networkError(Error)

        var errorDescription: String? {
            switch self {
            case .missingAPIKey:
                return "API key not found"
            case .invalidResponse:
                return "Invalid response from server"
            case .apiError(let code, let message):
                return "API Error (\(code)): \(message)"
            case .networkError(let error):
                return "Network error: \(error.localizedDescription)"
            }
        }
    }
}