import Foundation

struct CerberasClient {
    let model: String
    let baseURL: String
    let apiKey: String

    init(model: String, baseURL: String = "https://api.cerebras.ai/v1", apiKey: String?) {
        self.model = model
        self.baseURL = baseURL
        guard let apiKey, !apiKey.isEmpty else {
            fatalError("CerberasClient: missing API key")
        }
        self.apiKey = apiKey
    }

    func sendMessage(_ message: String, onUpdate: @escaping (String) -> Void) async throws -> String {
        let url = URL(string: "\(baseURL)/chat/completions")!
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.setValue("Bearer \(apiKey)", forHTTPHeaderField: "Authorization")

        let body: [String: Any] = [
            "model": model,
            "messages": [
                ["role": "user", "content": message]
            ],
            "temperature": 0.7
        ]

        request.httpBody = try JSONSerialization.data(withJSONObject: body)

        let (data, response) = try await URLSession.shared.data(for: request)

        guard let httpResponse = response as? HTTPURLResponse else {
            throw CerberasError.invalidResponse
        }

        guard httpResponse.statusCode == 200 else {
            throw CerberasError.apiError(httpResponse.statusCode, String(decoding: data, as: UTF8.self))
        }

        let responseText = try parseResponseText(from: data)
        onUpdate(responseText)
        return responseText
    }

    private func parseResponseText(from data: Data) throws -> String {
        struct ChatCompletion: Decodable {
            struct Choice: Decodable {
                struct Message: Decodable {
                    let content: String
                }
                let message: Message?
                let text: String?
            }
            let choices: [Choice]
        }

        if let completion = try? JSONDecoder().decode(ChatCompletion.self, from: data) {
            if let text = completion.choices.first?.message?.content, !text.isEmpty {
                return text
            }
            if let text = completion.choices.first?.text, !text.isEmpty {
                return text
            }
        }

        if let json = try JSONSerialization.jsonObject(with: data) as? [String: Any] {
            if let response = json["response"] as? String {
                return response
            }
            if let output = json["output"] as? [[String: Any]],
               let text = output.first?["content"] as? String {
                return text
            }
        }

        return String(decoding: data, as: UTF8.self)
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
