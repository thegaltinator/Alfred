import Foundation

struct CerberasClient {
    let model: String
    let baseURL: String
    let apiKey: String

    init?(model: String, baseURL: String = "https://api.cerebras.ai/v1", apiKey: String?) {
        guard let apiKey, !apiKey.isEmpty else { return nil }
        self.model = model
        self.baseURL = baseURL
        self.apiKey = apiKey
    }

    func streamMessage(_ message: String, onUpdate: @escaping (String) -> Void) async throws -> String {
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
            "temperature": 0.7,
            "stream": true
        ]

        request.httpBody = try JSONSerialization.data(withJSONObject: body)

        let (bytes, response) = try await URLSession.shared.bytes(for: request)
        guard let httpResponse = response as? HTTPURLResponse else {
            throw CerberasError.invalidResponse
        }
        guard httpResponse.statusCode == 200 else {
            throw CerberasError.apiError(httpResponse.statusCode, "non-200 response from Cerberas")
        }

        var finalText = ""
        for try await line in bytes.lines {
            let trimmed = line.trimmingCharacters(in: .whitespaces)
            guard trimmed.hasPrefix("data:") else { continue }

            let payload = trimmed.dropFirst(5).trimmingCharacters(in: .whitespaces)
            if payload == "[DONE]" { break }
            guard let chunkData = payload.data(using: .utf8) else { continue }

            if let chunk = try? JSONDecoder().decode(StreamChunk.self, from: chunkData) {
                if let delta = chunk.choices.first?.delta?.content ?? chunk.choices.first?.text, !delta.isEmpty {
                    finalText.append(delta)
                    onUpdate(finalText)
                }
            }
        }
        return finalText
    }

    private struct StreamChunk: Decodable {
        struct Choice: Decodable {
            struct Delta: Decodable {
                let content: String?
            }
            let delta: Delta?
            let text: String?
        }
        let choices: [Choice]
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
