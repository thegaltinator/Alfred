import Foundation

/// Chunker - Text chunking for embeddings
/// Breaks down text into optimal chunks for embedding generation and search
public class Chunker {

    // MARK: - Properties

    /// Maximum chunk size in tokens (approximate)
    private let maxChunkTokens: Int

    /// Overlap between chunks to maintain context
    private let overlapTokens: Int

    /// Characters per token (rough estimate)
    private let charsPerToken: Int = 4

    // MARK: - Initialization

    /// Initialize chunker with specified parameters
    /// - Parameters:
    ///   - maxChunkTokens: Maximum tokens per chunk (default: 256)
    ///   - overlapTokens: Token overlap between chunks (default: 50)
    public init(maxChunkTokens: Int = 256, overlapTokens: Int = 50) {
        self.maxChunkTokens = maxChunkTokens
        self.overlapTokens = min(overlapTokens, maxChunkTokens / 2) // Prevent too much overlap
    }

    // MARK: - Chunking Methods

    /// Split text into chunks for embedding
    /// - Parameters:
    ///   - text: Input text to chunk
    ///   - preserveSentences: Whether to preserve sentence boundaries
    /// - Returns: Array of text chunks
    public func chunkText(_ text: String, preserveSentences: Bool = true) -> [Chunk] {
        guard !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else {
            return []
        }

        let cleanText = text.trimmingCharacters(in: .whitespacesAndNewlines)

        if preserveSentences {
            return chunkBySentences(cleanText)
        } else {
            return chunkByTokens(cleanText)
        }
    }

    /// Split text by sentences with semantic boundaries
    /// - Parameter text: Input text
    /// - Returns: Array of sentence-based chunks
    private func chunkBySentences(_ text: String) -> [Chunk] {
        var chunks: [Chunk] = []
        let sentences = splitIntoSentences(text)
        var currentChunkSentences: [String] = []
        var currentTokens = 0

        for sentence in sentences {
            let sentenceTokens = estimateTokens(sentence)

            // If adding this sentence exceeds the limit and we have content, create a chunk
            if currentTokens + sentenceTokens > maxChunkTokens && !currentChunkSentences.isEmpty {
                let chunkText = currentChunkSentences.joined(separator: " ").trimmingCharacters(in: .whitespacesAndNewlines)
                let chunk = Chunk(
                    text: chunkText,
                    startIndex: chunks.count,
                    tokenCount: currentTokens,
                    metadata: [
                        "type": "sentence_based",
                        "sentence_count": currentChunkSentences.count
                    ]
                )
                chunks.append(chunk)

                // Start new chunk with overlap
                currentChunkSentences = createOverlapSentences(currentChunkSentences)
                currentTokens = currentChunkSentences.map { estimateTokens($0) }.reduce(0, +)
            }

            currentChunkSentences.append(sentence)
            currentTokens += sentenceTokens
        }

        // Add final chunk if we have remaining content
        if !currentChunkSentences.isEmpty {
            let chunkText = currentChunkSentences.joined(separator: " ").trimmingCharacters(in: .whitespacesAndNewlines)
            let chunk = Chunk(
                text: chunkText,
                startIndex: chunks.count,
                tokenCount: currentTokens,
                metadata: [
                    "type": "sentence_based",
                    "sentence_count": currentChunkSentences.count
                ]
            )
            chunks.append(chunk)
        }

        return chunks
    }

    /// Split text by token count with sliding window
    /// - Parameter text: Input text
    /// - Returns: Array of token-based chunks
    private func chunkByTokens(_ text: String) -> [Chunk] {
        var chunks: [Chunk] = []
        let charsPerChunk = maxChunkTokens * charsPerToken
        let overlapChars = overlapTokens * charsPerToken
        let textLength = text.count

        var startIndex = 0
        var chunkIndex = 0

        while startIndex < textLength {
            let endIndex = min(startIndex + charsPerChunk, textLength)
            let chunkText = String(text[text.index(text.startIndex, offsetBy: startIndex)..<text.index(text.startIndex, offsetBy: endIndex)])
                .trimmingCharacters(in: .whitespacesAndNewlines)

            if !chunkText.isEmpty {
                let estimatedTokens = estimateTokens(chunkText)
                let chunk = Chunk(
                    text: chunkText,
                    startIndex: chunkIndex,
                    tokenCount: estimatedTokens,
                    metadata: [
                        "type": "token_based",
                        "char_start": startIndex,
                        "char_end": endIndex
                    ]
                )
                chunks.append(chunk)
                chunkIndex += 1
            }

            // Move start index with overlap
            startIndex = max(0, endIndex - overlapChars)
        }

        return chunks
    }

    // MARK: - Helper Methods

    /// Split text into sentences using basic punctuation
    /// - Parameter text: Input text
    /// - Returns: Array of sentences
    private func splitIntoSentences(_ text: String) -> [String] {
        // Simple sentence splitting - can be enhanced with more sophisticated NLP
        let sentences = text.components(separatedBy: CharacterSet(charactersIn: ".!?"))
            .map { $0.trimmingCharacters(in: .whitespacesAndNewlines) }
            .filter { !$0.isEmpty }
        return sentences
    }

    /// Create overlap sentences for next chunk
    /// - Parameter sentences: Current chunk sentences
    /// - Returns: Sentences to overlap into next chunk
    private func createOverlapSentences(_ sentences: [String]) -> [String] {
        guard sentences.count > 1 else { return [] }

        var overlapTokens = 0
        var overlapSentences: [String] = []

        // Take sentences from the end until we reach overlap token limit
        for sentence in sentences.reversed() {
            let sentenceTokens = estimateTokens(sentence)
            if overlapTokens + sentenceTokens <= overlapTokens {
                overlapSentences.insert(sentence, at: 0)
                overlapTokens += sentenceTokens
            } else {
                break
            }
        }

        return overlapSentences
    }

    /// Estimate token count for text (rough approximation)
    /// - Parameter text: Input text
    /// - Returns: Estimated token count
    private func estimateTokens(_ text: String) -> Int {
        // Simple estimation: characters / charsPerToken, rounded up
        return max(1, (text.count + charsPerToken - 1) / charsPerToken)
    }

    // MARK: - Utility Methods

    /// Get optimal chunk size for specific content type
    /// - Parameter contentType: Type of content (note, email, document, etc.)
    /// - Returns: Recommended max tokens per chunk
    public func getOptimalChunkSize(for contentType: ContentType) -> Int {
        switch contentType {
        case .note:
            return 256
        case .email:
            return 512
        case .document:
            return 1024
        case .chat:
            return 128
        }
    }
}

// MARK: - Data Models

/// Represents a text chunk for embedding
public struct Chunk {
    /// The chunk text content
    public let text: String

    /// Index of this chunk in the original text
    public let startIndex: Int

    /// Estimated token count
    public let tokenCount: Int

    /// Additional metadata about the chunk
    public let metadata: [String: Any]

    /// Initialize a new chunk
    /// - Parameters:
    ///   - text: Chunk text
    ///   - startIndex: Chunk index
    ///   - tokenCount: Token count
    ///   - metadata: Additional metadata
    public init(text: String, startIndex: Int, tokenCount: Int, metadata: [String: Any] = [:]) {
        self.text = text
        self.startIndex = startIndex
        self.tokenCount = tokenCount
        self.metadata = metadata
    }
}

/// Content types for chunking optimization
public enum ContentType {
    case note
    case email
    case document
    case chat
}