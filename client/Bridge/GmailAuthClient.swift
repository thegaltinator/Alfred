import Foundation
import Network

/// Gmail authentication client for handling OAuth2 flows with the server
/// This client is intentionally simple - all OAuth state management is server-side
public actor GmailAuthClient {

    // MARK: - Types

    public enum Service: String, CaseIterable, Codable {
        case gmail = "gmail"
        case calendar = "calendar"
    }

    public enum AuthError: Error, LocalizedError {
        case invalidURL
        case networkError(Error)
        case serverError(Int, String)
        case decodingError(Error)
        case authenticationFailed(String)

        public var errorDescription: String? {
            switch self {
            case .invalidURL:
                return "Invalid server URL"
            case .networkError(let error):
                return "Network error: \(error.localizedDescription)"
            case .serverError(let code, let message):
                return "Server error \(code): \(message)"
            case .decodingError(let error):
                return "Failed to decode response: \(error.localizedDescription)"
            case .authenticationFailed(let message):
                return "Authentication failed: \(message)"
            }
        }
    }

    public struct AuthRequest: Codable {
        let userID: String
        let service: Service
    }

    public struct AuthResponse: Codable {
        let authURL: String
        let state: String
    }

    public struct CallbackResponse: Codable {
        let success: Bool
        let message: String
        let service: String?
    }

    public struct StatusResponse: Codable {
        let userID: String
        let services: [String: String]
    }

    private struct ValidationResult: Codable {
        let valid: Bool
        let error: String?
    }

    // MARK: - Properties

    private let baseURL: URL
    private let session: URLSession

    // MARK: - Initialization

    public init(baseURL: URL = URL(string: "http://localhost:8080")!) {
        self.baseURL = baseURL
        self.session = URLSession.shared
    }

    // MARK: - Public Methods

    /// Initiate OAuth authentication for a specific service
    /// - Parameters:
    ///   - userID: The user identifier
    ///   - service: The service to authenticate (gmail or calendar)
    /// - Returns: Authentication URL and state parameter
    public func initiateAuth(userID: String, service: Service) async throws -> (authURL: URL, state: String) {
        let request = AuthRequest(userID: userID, service: service)
        let response = try await performRequest(
            endpoint: "/auth/google",
            method: "POST",
            body: request,
            responseType: AuthResponse.self
        )

        guard let authURL = URL(string: response.authURL) else {
            throw AuthError.invalidURL
        }

        return (authURL, response.state)
    }

    /// Check authentication status for all services
    /// - Parameter userID: The user identifier
    /// - Returns: Status of all authenticated services
    public func checkStatus(userID: String) async throws -> StatusResponse {
        var components = URLComponents(url: baseURL.appendingPathComponent("/auth/status"), resolvingAgainstBaseURL: false)!
        components.queryItems = [URLQueryItem(name: "user_id", value: userID)]

        guard let url = components.url else {
            throw AuthError.invalidURL
        }

        let (data, response) = try await session.data(from: url)

        guard let httpResponse = response as? HTTPURLResponse else {
            throw AuthError.networkError(URLError(.badServerResponse))
        }

        guard 200...299 ~= httpResponse.statusCode else {
            let errorMessage = String(data: data, encoding: .utf8) ?? "Unknown error"
            throw AuthError.serverError(httpResponse.statusCode, errorMessage)
        }

        do {
            return try JSONDecoder().decode(StatusResponse.self, from: data)
        } catch {
            throw AuthError.decodingError(error)
        }
    }

    /// Validate authentication for a specific service
    /// - Parameters:
    ///   - userID: The user identifier
    ///   - service: The service to validate
    /// - Returns: Whether the service is valid and any error message
    public func validateService(userID: String, service: Service) async throws -> (valid: Bool, error: String?) {
        var components = URLComponents(url: baseURL.appendingPathComponent("/auth/validate/\(service.rawValue)"), resolvingAgainstBaseURL: false)!
        components.queryItems = [URLQueryItem(name: "user_id", value: userID)]

        guard let url = components.url else {
            throw AuthError.invalidURL
        }

        let (data, response) = try await session.data(from: url)

        guard let httpResponse = response as? HTTPURLResponse else {
            throw AuthError.networkError(URLError(.badServerResponse))
        }

        guard 200...299 ~= httpResponse.statusCode else {
            let errorMessage = String(data: data, encoding: .utf8) ?? "Unknown error"
            throw AuthError.serverError(httpResponse.statusCode, errorMessage)
        }

        do {
        let result = try JSONDecoder().decode(ValidationResult.self, from: data)
        return (result.valid, result.error)
    } catch {
        throw AuthError.decodingError(error)
    }
    }

    /// Revoke authentication for a specific service
    /// - Parameters:
    ///   - userID: The user identifier
    ///   - service: The service to revoke
    public func revokeAccess(userID: String, service: Service) async throws {
        var components = URLComponents(url: baseURL.appendingPathComponent("/auth/revoke/\(service.rawValue)"), resolvingAgainstBaseURL: false)!
        components.queryItems = [URLQueryItem(name: "user_id", value: userID)]

        guard let url = components.url else {
            throw AuthError.invalidURL
        }

        var request = URLRequest(url: url)
        request.httpMethod = "DELETE"

        let (_, response) = try await session.data(for: request)

        guard let httpResponse = response as? HTTPURLResponse else {
            throw AuthError.networkError(URLError(.badServerResponse))
        }

        guard 200...299 ~= httpResponse.statusCode else {
            let errorMessage = String(data: try await session.data(from: url).0, encoding: .utf8) ?? "Unknown error"
            throw AuthError.serverError(httpResponse.statusCode, errorMessage)
        }
    }

    // MARK: - Private Methods

    private func performRequest<RequestType: Codable, ResponseType: Codable>(
        endpoint: String,
        method: String,
        body: RequestType,
        responseType: ResponseType.Type
    ) async throws -> ResponseType {
        let url = baseURL.appendingPathComponent(endpoint)

        var request = URLRequest(url: url)
        request.httpMethod = method
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")

        do {
            request.httpBody = try JSONEncoder().encode(body)
        } catch {
            throw AuthError.decodingError(error)
        }

        let (data, response) = try await session.data(for: request)

        guard let httpResponse = response as? HTTPURLResponse else {
            throw AuthError.networkError(URLError(.badServerResponse))
        }

        guard 200...299 ~= httpResponse.statusCode else {
            let errorMessage = String(data: data, encoding: .utf8) ?? "Unknown error"
            throw AuthError.serverError(httpResponse.statusCode, errorMessage)
        }

        do {
            return try JSONDecoder().decode(ResponseType.self, from: data)
        } catch {
            throw AuthError.decodingError(error)
        }
    }
}

// MARK: - Convenience Extensions

extension GmailAuthClient {

    /// Initiate Gmail authentication
    /// - Parameter userID: The user identifier
    /// - Returns: Authentication URL and state parameter
    public func authenticateGmail(userID: String) async throws -> (authURL: URL, state: String) {
        return try await initiateAuth(userID: userID, service: .gmail)
    }

    /// Initiate Calendar authentication
    /// - Parameter userID: The user identifier
    /// - Returns: Authentication URL and state parameter
    public func authenticateCalendar(userID: String) async throws -> (authURL: URL, state: String) {
        return try await initiateAuth(userID: userID, service: .calendar)
    }

    /// Check if Gmail is authenticated and valid
    /// - Parameter userID: The user identifier
    /// - Returns: Whether Gmail is authenticated and valid
    public func isGmailAuthenticated(userID: String) async throws -> Bool {
        let (valid, _) = try await validateService(userID: userID, service: .gmail)
        return valid
    }

    /// Check if Calendar is authenticated and valid
    /// - Parameter userID: The user identifier
    /// - Returns: Whether Calendar is authenticated and valid
    public func isCalendarAuthenticated(userID: String) async throws -> Bool {
        let (valid, _) = try await validateService(userID: userID, service: .calendar)
        return valid
    }
}

// MARK: - Testing Support

#if DEBUG
/// Protocol to allow dependency injection for testing
public protocol GmailAuthProtocol {
    func initiateAuth(userID: String, service: GmailAuthClient.Service) async throws -> (authURL: URL, state: String)
    func checkStatus(userID: String) async throws -> GmailAuthClient.StatusResponse
    func validateService(userID: String, service: GmailAuthClient.Service) async throws -> (valid: Bool, error: String?)
    func revokeAccess(userID: String, service: GmailAuthClient.Service) async throws
}

extension GmailAuthClient: GmailAuthProtocol {}

/// Mock implementation for testing
public actor MockGmailAuthClient: GmailAuthProtocol {

    private var mockAuthenticatedServices: [String: Set<GmailAuthClient.Service>] = [:]

    public func initiateAuth(userID: String, service: GmailAuthClient.Service) async throws -> (authURL: URL, state: String) {
        let state = "mock_state_\(UUID().uuidString)"
        let authURL = URL(string: "https://accounts.google.com/oauth/authorize?mock=true&state=\(state)")!
        return (authURL, state)
    }

    public func checkStatus(userID: String) async throws -> GmailAuthClient.StatusResponse {
        let services = mockAuthenticatedServices[userID] ?? []
        var serviceStatus: [String: String] = [:]

        for service in GmailAuthClient.Service.allCases {
            if services.contains(service) {
                serviceStatus[service.rawValue] = "valid"
            } else {
                serviceStatus[service.rawValue] = "not_authenticated"
            }
        }

        return GmailAuthClient.StatusResponse(userID: userID, services: serviceStatus)
    }

    public func validateService(userID: String, service: GmailAuthClient.Service) async throws -> (valid: Bool, error: String?) {
        let services = mockAuthenticatedServices[userID] ?? []
        let isValid = services.contains(service)
        return (isValid, isValid ? nil : "Service not authenticated")
    }

    public func revokeAccess(userID: String, service: GmailAuthClient.Service) async throws {
        mockAuthenticatedServices[userID]?.remove(service)
    }

    // MARK: - Test Helpers

    /// Simulate successful authentication for testing
    public func simulateSuccessfulAuth(userID: String, service: GmailAuthClient.Service) {
        if mockAuthenticatedServices[userID] == nil {
            mockAuthenticatedServices[userID] = []
        }
        mockAuthenticatedServices[userID]?.insert(service)
    }

    /// Clear all mock authentication state
    public func clearMockState() {
        mockAuthenticatedServices.removeAll()
    }
}
#endif