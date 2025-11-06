import Foundation
import Heartbeat

print("🧪 Testing Heartbeat module directly...")

let environment = Environment()
let heartbeatClient = HeartbeatClient(baseURL: environment.cloudBaseURL)

heartbeatClient.start()

print("🫀 Heartbeat test running for 30 seconds...")
print("📍 Check Redis stream: redis-cli XREVRANGE user:dev:test:in:prod + - COUNT 5")

RunLoop.main.run(until: Date(timeIntervalSinceNow: 30))

heartbeatClient.stop()
print("✅ Test completed")