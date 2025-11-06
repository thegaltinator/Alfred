import CoreGraphics
import Foundation

struct IdleTimeMonitor {
    func idleSeconds() -> Int {
        let seconds = CGEventSource.secondsSinceLastEventType(.hidSystemState, eventType: .mouseMoved)
        if seconds.isFinite && seconds >= 0 {
            return Int(seconds)
        }
        return 0
    }
}
