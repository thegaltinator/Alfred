import Foundation

/// Performance benchmark suite for memory system optimizations
/// Tests CPU usage, response times, and cache effectiveness
class MemoryPerformanceBenchmark {

    // MARK: - Properties

    private let memoryBridge: MemoryBridge
    private let testIterations = 20
    private let testTexts = [
        "What's my schedule for today?",
        "I need to remember to buy groceries",
        "Can you help me with my email?",
        "What meetings do I have this week?",
        "Remind me to call my mom",
        "What was I working on yesterday?",
        "I need to finish the project report",
        "Can you check my calendar availability?",
        "What important tasks do I have?",
        "I should remember this conversation"
    ]

    // MARK: - Initialization

    init() throws {
        self.memoryBridge = try MemoryBridge()
    }

    // MARK: - Benchmark Methods

    /// Run comprehensive performance benchmarks
    func runAllBenchmarks() async throws -> PerformanceReport {
        print("🚀 Starting Memory Performance Benchmarks...")

        // Warm up the system
        try await warmUpSystem()

        // Test 1: Embedding generation performance
        let embeddingReport = await benchmarkEmbeddingGeneration()

        // Test 2: Memory search performance
        let searchReport = await benchmarkMemorySearch()

        // Test 3: Cache effectiveness
        let cacheReport = await benchmarkCacheEffectiveness()

        // Test 4: CPU usage simulation
        let cpuReport = await benchmarkCPUUsage()

        // Generate comprehensive report
        let report = PerformanceReport(
            embeddingPerformance: embeddingReport,
            searchPerformance: searchReport,
            cacheEffectiveness: cacheReport,
            cpuUsage: cpuReport
        )

        print("✅ Memory Performance Benchmarks completed")
        return report
    }

    /// Warm up the system to eliminate first-run overhead
    private func warmUpSystem() async throws {
        print("🔥 Warming up system...")
        _ = try await memoryBridge.processTranscript("Warm up text")
        _ = try await memoryBridge.searchMemories("test")
        print("✅ System warmed up")
    }

    /// Benchmark embedding generation performance
    private func benchmarkEmbeddingGeneration() async -> EmbeddingPerformanceReport {
        print("⏱️ Benchmarking embedding generation...")

        var times: [Double] = []
        let startTime = CFAbsoluteTimeGetCurrent()

        for text in testTexts {
            let iterationStart = CFAbsoluteTimeGetCurrent()
            _ = try? await memoryBridge.processTranscript(text)
            let iterationTime = (CFAbsoluteTimeGetCurrent() - iterationStart) * 1000
            times.append(iterationTime)
        }

        let totalTime = CFAbsoluteTimeGetCurrent() - startTime

        return EmbeddingPerformanceReport(
            totalTime: totalTime,
            averageTimePerEmbedding: times.reduce(0, +) / Double(times.count),
            minTime: times.min() ?? 0,
            maxTime: times.max() ?? 0,
            medianTime: calculateMedian(times),
            throughputPerSecond: Double(testTexts.count) / totalTime
        )
    }

    /// Benchmark memory search performance
    private func benchmarkMemorySearch() async -> SearchPerformanceReport {
        print("🔍 Benchmarking memory search...")

        var times: [Double] = []
        var resultCounts: [Int] = []

        let searchQueries = [
            "schedule", "meetings", "tasks", "project", "important", "work", "remember", "help"
        ]

        for query in searchQueries {
            let iterationStart = CFAbsoluteTimeGetCurrent()
            let results = try? await memoryBridge.searchMemories(query, limit: 5)
            let iterationTime = (CFAbsoluteTimeGetCurrent() - iterationStart) * 1000
            times.append(iterationTime)
            resultCounts.append(results?.count ?? 0)
        }

        return SearchPerformanceReport(
            averageSearchTime: times.reduce(0, +) / Double(times.count),
            minSearchTime: times.min() ?? 0,
            maxSearchTime: times.max() ?? 0,
            medianSearchTime: calculateMedian(times),
            averageResultCount: resultCounts.reduce(0, +) / Double(resultCounts.count)
        )
    }

    /// Benchmark cache effectiveness
    private func benchmarkCacheEffectiveness() async -> CacheEffectivenessReport {
        print("🎯 Benchmarking cache effectiveness...")

        // Clear caches first
        memoryBridge.clearTranscriptCache()

        // First pass - should be cache misses
        let firstPassStart = CFAbsoluteTimeGetCurrent()
        for text in Array(testTexts.prefix(5)) {
            _ = try? await memoryBridge.processTranscript(text)
        }
        let firstPassTime = CFAbsoluteTimeGetCurrent() - firstPassStart

        // Second pass - should be cache hits
        let secondPassStart = CFAbsoluteTimeGetCurrent()
        for text in Array(testTexts.prefix(5)) {
            _ = try? await memoryBridge.processTranscript(text)
        }
        let secondPassTime = CFAbsoluteTimeGetCurrent() - secondPassStart

        let speedupRatio = firstPassTime / secondPassTime

        return CacheEffectivenessReport(
            firstPassTime: firstPassTime * 1000,
            secondPassTime: secondPassTime * 1000,
            speedupRatio: speedupRatio,
            cacheHitRate: speedupRatio > 1 ? (speedupRatio - 1) / speedupRatio : 0
        )
    }

    /// Simulate CPU usage during heavy memory operations
    private func benchmarkCPUUsage() async -> CPUUsageReport {
        print("💻 Benchmarking CPU usage...")

        let concurrentTasks = 5
        let operationsPerTask = 10

        let startTime = CFAbsoluteTimeGetCurrent()

        // Simulate concurrent memory operations
        await withTaskGroup(of: Void.self) { group in
            for _ in 0..<concurrentTasks {
                group.addTask {
                    for _ in 0..<operationsPerTask {
                        let randomText = self.testTexts.randomElement() ?? "test"
                        _ = try? await self.memoryBridge.processTranscript(randomText)

                        // Small delay to simulate realistic usage
                        await Task.sleep(nanoseconds: 50_000_000) // 50ms
                    }
                }
            }
        }

        let totalTime = CFAbsoluteTimeGetCurrent() - startTime
        let totalOperations = concurrentTasks * operationsPerTask

        return CPUUsageReport(
            totalTime: totalTime,
            totalOperations: totalOperations,
            operationsPerSecond: Double(totalOperations) / totalTime,
            averageTimePerOperation: (totalTime * 1000) / Double(totalOperations)
        )
    }

    /// Calculate median value from array of doubles
    private func calculateMedian(_ values: [Double]) -> Double {
        guard !values.isEmpty else { return 0 }

        let sorted = values.sorted()
        let count = sorted.count

        if count % 2 == 0 {
            return (sorted[count/2 - 1] + sorted[count/2]) / 2
        } else {
            return sorted[count/2]
        }
    }
}

// MARK: - Performance Report Models

struct PerformanceReport {
    let embeddingPerformance: EmbeddingPerformanceReport
    let searchPerformance: SearchPerformanceReport
    let cacheEffectiveness: CacheEffectivenessReport
    let cpuUsage: CPUUsageReport

    func printSummary() {
        print("\n📊 PERFORMANCE BENCHMARK SUMMARY")
        print("=" * 50)

        print("\n🧠 EMBEDDING PERFORMANCE:")
        print("  Average time: \(String(format: "%.2f", embeddingPerformance.averageTimePerEmbedding))ms")
        print("  Min time: \(String(format: "%.2f", embeddingPerformance.minTime))ms")
        print("  Max time: \(String(format: "%.2f", embeddingPerformance.maxTime))ms")
        print("  Throughput: \(String(format: "%.2f", embeddingPerformance.throughputPerSecond)) ops/sec")

        print("\n🔍 SEARCH PERFORMANCE:")
        print("  Average search time: \(String(format: "%.2f", searchPerformance.averageSearchTime))ms")
        print("  Average results: \(String(format: "%.1f", searchPerformance.averageResultCount))")

        print("\n🎯 CACHE EFFECTIVENESS:")
        print("  First pass time: \(String(format: "%.2f", cacheEffectiveness.firstPassTime))ms")
        print("  Second pass time: \(String(format: "%.2f", cacheEffectiveness.secondPassTime))ms")
        print("  Speedup ratio: \(String(format: "%.2f", cacheEffectiveness.speedupRatio))x")

        print("\n💻 CPU USAGE:")
        print("  Operations per second: \(String(format: "%.2f", cpuUsage.operationsPerSecond))")
        print("  Average time per operation: \(String(format: "%.2f", cpuUsage.averageTimePerOperation))ms")

        print("\n" + "=" * 50)
    }
}

struct EmbeddingPerformanceReport {
    let totalTime: Double
    let averageTimePerEmbedding: Double
    let minTime: Double
    let maxTime: Double
    let medianTime: Double
    let throughputPerSecond: Double
}

struct SearchPerformanceReport {
    let averageSearchTime: Double
    let minSearchTime: Double
    let maxSearchTime: Double
    let medianSearchTime: Double
    let averageResultCount: Double
}

struct CacheEffectivenessReport {
    let firstPassTime: Double
    let secondPassTime: Double
    let speedupRatio: Double
    let cacheHitRate: Double
}

struct CPUUsageReport {
    let totalTime: Double
    let totalOperations: Int
    let operationsPerSecond: Double
    let averageTimePerOperation: Double
}

// String extension for repeat operator
extension String {
    static func *(lhs: String, rhs: Int) -> String {
        return String(repeating: lhs, count: rhs)
    }
}