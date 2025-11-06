import Foundation

final class Presence {
    enum Mode: String { case talkative, subtle, silent }

    private let defaults = UserDefaults.standard
    private let modeKey = "PresenceMode"
    private let focusKey = "PresenceFocus"
    private let callKey = "PresenceCall"
    private let speakLogKey = "PresenceSpeakLog"

    var maxSpokenPerHour: Int = 4

    var mode: Mode {
        didSet { defaults.set(mode.rawValue, forKey: modeKey) }
    }
    var focusOn: Bool { didSet { defaults.set(focusOn, forKey: focusKey) } }
    var callActive: Bool { didSet { defaults.set(callActive, forKey: callKey) } }

    init() {
        if let raw = defaults.string(forKey: modeKey), let m = Mode(rawValue: raw) {
            mode = m
        } else { mode = .talkative }
        focusOn = defaults.bool(forKey: focusKey)
        callActive = defaults.bool(forKey: callKey)
    }

    func canSpeak(now: Date = Date()) -> Bool {
        if focusOn || callActive { return false }
        if mode == .silent { return false }
        return spokenInLastHour(now: now) < maxSpokenPerHour
    }

    func recordSpeak(now: Date = Date()) {
        var times = speakTimes()
        times.append(now.timeIntervalSince1970)
        defaults.set(times, forKey: speakLogKey)
    }

    func modeString() -> String { return mode.rawValue }

    private func speakTimes() -> [TimeInterval] {
        return defaults.array(forKey: speakLogKey) as? [TimeInterval] ?? []
    }

    private func spokenInLastHour(now: Date = Date()) -> Int {
        let cutoff = now.addingTimeInterval(-3600).timeIntervalSince1970
        let filtered = speakTimes().filter { $0 >= cutoff }
        defaults.set(filtered, forKey: speakLogKey)
        return filtered.count
    }
}


