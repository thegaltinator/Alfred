import Foundation
import Memory

@main
enum TestMemoryOrchestrator {
    static func main() async {
        print("🧪 Testing Python Memory Orchestrator System")

        do {
            // Initialize the memory bridge
            let bridge = try MemoryBridge()
            print("✅ MemoryBridge initialized successfully")

            // Test 1: Add some memories
            let memory1 = try await bridge.addMemory(
                "User prefers working in the morning and usually drinks coffee",
                metadata: ["type": "preference", "time": "morning"]
            )
            print("✅ Added memory 1: \(memory1)")

            let memory2 = try await bridge.addMemory(
                "User is learning Swift programming and struggling with concurrency",
                metadata: ["type": "learning", "topic": "swift"]
            )
            print("✅ Added memory 2: \(memory2)")

            let memory3 = try await bridge.addMemory(
                "User has a meeting every Friday at 2 PM with the team",
                metadata: ["type": "schedule", "frequency": "weekly"]
            )
            print("✅ Added memory 3: \(memory3)")

            // Test 2: Search memories
            let searchResults = try await bridge.searchMemories("Swift programming", limit: 2)
            print("✅ Search for 'Swift programming' found \(searchResults.count) results:")
            for (index, result) in searchResults.enumerated() {
                print("   \(index + 1). \(result.content.prefix(50))... (similarity: \(String(format: "%.2f", result.similarity)))")
            }

            // Test 3: Process transcript with memory context
            let transcript = "I'm having trouble with Swift concurrency again"
            print("\n🎯 Processing transcript: '\(transcript)'")

            let response = try await bridge.processTranscript(transcript)
            print("📝 Speech Plan: \(response.speechPlan)")
            print("🔍 Retrieved \(response.retrievedMemories.count) memories:")
            for (index, memory) in response.retrievedMemories.enumerated() {
                print("   \(index + 1). \(memory.content.prefix(60))... (similarity: \(String(format: "%.2f", memory.similarity)))")
            }
            print("⏱️ Processing Time: \(String(format: "%.0f", response.processingTimeMs))ms")

            // Test 4: Process transcript without relevant memories
            let unrelatedTranscript = "What's the weather like today?"
            print("\n🎯 Processing transcript: '\(unrelatedTranscript)'")

            let unrelatedResponse = try await bridge.processTranscript(unrelatedTranscript)
            print("📝 Speech Plan: \(unrelatedResponse.speechPlan)")
            print("🔍 Retrieved \(unrelatedResponse.retrievedMemories.count) memories")
            print("⏱️ Processing Time: \(String(format: "%.0f", unrelatedResponse.processingTimeMs))ms")

            print("\n✅ All tests completed successfully!")
            print("🎉 Python Memory Orchestrator system is working correctly")

        } catch {
            print("❌ Test failed: \(error.localizedDescription)")
            exit(1)
        }
    }
}