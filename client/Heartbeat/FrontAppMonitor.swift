import AppKit
import Foundation

struct FrontAppSnapshot {
    let bundleIdentifier: String?
    let name: String?
    let tabDomain: String?
    let tabTitle: String?

    var frontAppLabel: String? {
        if let name, !name.isEmpty {
            return name
        }
        return bundleIdentifier
    }
}

final class FrontAppMonitor {
    private let tabReader = ActiveTabReader()

    func snapshot() -> FrontAppSnapshot {
        if Thread.isMainThread {
            return captureSnapshot()
        }
        var snapshot = FrontAppSnapshot(bundleIdentifier: nil, name: nil, tabDomain: nil, tabTitle: nil)
        DispatchQueue.main.sync {
            snapshot = self.captureSnapshot()
        }
        return snapshot
    }

    private func captureSnapshot() -> FrontAppSnapshot {
        guard let app = NSWorkspace.shared.frontmostApplication else {
            print("[FrontAppMonitor] No frontmost application found")
            return FrontAppSnapshot(bundleIdentifier: nil, name: nil, tabDomain: nil, tabTitle: nil)
        }
        let bundleId = app.bundleIdentifier
        let appName = app.localizedName
        print("[FrontAppMonitor] Front app: \(appName ?? "unknown") (\(bundleId ?? "unknown"))")

        let tabInfo = tabReader.snapshot(for: bundleId)
        if let tabInfo = tabInfo {
            print("[FrontAppMonitor] Tab info found - title: \(tabInfo.title ?? "nil"), url: \(tabInfo.url ?? "nil"), domain: \(tabInfo.domain ?? "nil")")
        } else {
            print("[FrontAppMonitor] No tab info returned for bundleId: \(bundleId ?? "nil")")
        }

        return FrontAppSnapshot(
            bundleIdentifier: bundleId,
            name: appName,
            tabDomain: tabInfo?.domain,
            tabTitle: tabInfo?.title
        )
    }
}
