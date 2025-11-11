import Foundation

/// Test runner for memory performance benchmarks
/// This can be executed to verify the performance improvements
class PerformanceTestRunner {

    static func main() async {
        print("🧪 Alfred Memory Performance Test Runner")
        print("=========================================")

        do {
            // Initialize benchmark suite
            let benchmark = try MemoryPerformanceBenchmark()

            // Run all benchmarks
            let report = try await benchmark.runAllBenchmarks()

            // Print detailed results
            report.printSummary()

            // Print optimization recommendations
            printOptimizationRecommendations(report: report)

        } catch {
            print("❌ Performance tests failed: \(error.localizedDescription)")
        }
    }

    private static func printOptimizationRecommendations(report: PerformanceReport) {
        print("\n💡 OPTIMIZATION RECOMMENDATIONS")
        print("--------------------------------")

        // Embedding performance recommendations
        if report.embeddingPerformance.averageTimePerEmbedding > 100 {
            print("⚠️  Embedding generation is slow (>100ms). Consider:")
            print("   - Using CoreML acceleration if available")
            print("   - Reducing model size or quantization")
            print("   - Implementing better process pooling")
        } else {
            print("✅ Embedding performance is good (<100ms average)")
        }

        // Search performance recommendations
        if report.searchPerformance.averageSearchTime > 50 {
            print("⚠️  Memory search is slow (>50ms). Consider:")
            print("   - Increasing vector cache size")
            print("   - Using sqlite-vec extension for better indexing")
            print("   - Optimizing cosine similarity calculations")
        } else {
            print("✅ Search performance is good (<50ms average)")
        }

        // Cache effectiveness recommendations
        if report.cacheEffectiveness.speedupRatio < 2.0 {
            print("⚠️  Cache effectiveness is low (<2x speedup). Consider:")
            print("   - Increasing cache sizes")
            print("   - Implementing smarter cache eviction policies")
            print("   - Adding cache preloading for common queries")
        } else {
            print("✅ Cache effectiveness is good (>2x speedup)")
        }

        // CPU usage recommendations
        if report.cpuUsage.averageTimePerOperation > 200 {
            print("⚠️  High CPU usage detected (>200ms per operation). Consider:")
            print("   - Reducing concurrent operations")
            print("   - Implementing better batching")
            print("   - Using more efficient data structures")
        } else {
            print("✅ CPU usage is reasonable (<200ms per operation)")
        }

        print("\n🎯 Expected Performance Targets:")
        print("   Embedding: <50ms average")
        print("   Search: <25ms average")
        print("   Cache speedup: >3x")
        print("   CPU per operation: <100ms")
    }
}

// MARK: - Quick Performance Check

/// Quick performance check that can be run during development
class QuickPerformanceCheck {

    static func runQuickCheck() async throws {
        print("⚡ Running quick performance check...")

        let memoryBridge = try MemoryBridge()
        let testText = "What's my schedule today?"

        // Test single embedding operation
        let start = CFAbsoluteTimeGetCurrent()
        _ = try await memoryBridge.processTranscript(testText)
        let time = (CFAbsoluteTimeGetCurrent() - start) * 1000

        print("⏱️  Single embedding time: \(String(format: "%.2f", time))ms")

        if time < 50 {
            print("✅ Performance is good!")
        } else if time < 100 {
            print("⚠️  Performance is acceptable but could be improved")
        } else {
            print("❌ Performance needs optimization")
        }

        // Test cache effectiveness
        let secondStart = CFAbsoluteTimeGetCurrent()
        _ = try await memoryBridge.processTranscript(testText)
        let secondTime = (CFAbsoluteTimeGetCurrent() - secondStart) * 1000

        let speedup = time / secondTime
        print("🎯 Cache speedup: \(String(format: "%.2f", speedup))x")

        if speedup > 3 {
            print("✅ Cache is working well!")
        } else {
            print("⚠️  Cache could be more effective")
        }
    }
}

// MARK: - Memory Stress Test

/// Stress test for memory system under load
class MemoryStressTest {

    static func runStressTest() async throws {
        print("🔥 Running memory stress test...")

        let memoryBridge = try MemoryBridge()
        let concurrentRequests = 10
        let requestsPerWorker = 20

        let startTime = CFAbsoluteTimeGetCurrent()

        await withTaskGroup(of: Double.self) { group in
            for _ in 0..<concurrentRequests {
                group.addTask {
                    let workerStartTime = CFAbsoluteTimeGetCurrent()

                    for i in 0..<requestsPerWorker {
                        let testText = "Test request \(i) from worker \(Task.currentTask)"
                        _ = try? await memoryBridge.processTranscript(testText)

                        // Small delay to simulate realistic usage
                        await Task.sleep(nanoseconds: 10_000_000) // 10ms
                    }

                    return CFAbsoluteTimeGetCurrent() - workerStartTime
                }
            }

            var workerTimes: [Double] = []
            for try await time in group {
                workerTimes.append(time)
            }

            let totalTime = CFAbsoluteTimeGetCurrent() - startTime
            let totalRequests = concurrentRequests * requestsPerWorker
            let avgWorkerTime = workerTimes.reduce(0, +) / Double(workerTimes.count)

            print("📊 Stress Test Results:")
            print("   Total time: \(String(format: "%.2f", totalTime))s")
            print("   Total requests: \(totalRequests)")
            print("   Requests per second: \(String(format: "%.2f", Double(totalRequests) / totalTime))")
            print("   Average worker time: \(String(format: "%.2f", avgWorkerTime))s")
            print("   Max worker time: \(String(format: "%.2f", workerTimes.max() ?? 0))s")
            print("   Min worker time: \(String(format: "%.2f", workerTimes.min() ?? 0))s")

            // Check for performance degradation under load
            let avgTimePerRequest = (totalTime * 1000) / Double(totalRequests)
            if avgTimePerRequest < 150 {
                print("✅ System handles concurrent load well")
            } else {
                print("⚠️  Performance degrades significantly under load")
            }
        }
    }
}