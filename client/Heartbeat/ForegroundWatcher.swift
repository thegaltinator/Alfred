import AppKit
import CoreGraphics

struct ForegroundSnapshot {
    let bundleID: String
    let windowTitle: String?
    let url: URL?

    // Derive a lightweight activity identifier when we have a title or URL.
    var activityID: String? {
        let title = windowTitle?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        let urlStr = url?.absoluteString ?? ""

        if !title.isEmpty {
            return "\(bundleID)#\(title.prefix(80))"
        } else if !urlStr.isEmpty {
            return "\(bundleID)#\(urlStr.prefix(80))"
        }
        return nil
    }
}

final class ForegroundWatcher {
    private let workspace = NSWorkspace.shared

    func snapshot() -> ForegroundSnapshot? {
        guard let app = workspace.frontmostApplication,
              let bundleID = app.bundleIdentifier else {
            return nil
        }

        let title = windowTitle(for: app.processIdentifier)
        let url = browserURL(for: bundleID)

        return ForegroundSnapshot(bundleID: bundleID, windowTitle: title, url: url)
    }

    private func windowTitle(for pid: pid_t) -> String? {
        // Use Accessibility API as specified in architecture
        // Get the frontmost window for the given process
        let options: CGWindowListOption = [.optionOnScreenOnly, .excludeDesktopElements]
        guard let windowList = CGWindowListCopyWindowInfo(options, kCGNullWindowID) as? [[String: Any]] else {
            return nil
        }

        // Find windows belonging to the target process, prioritized by layer/order
        var targetWindows: [[String: Any]] = []
        for window in windowList where (window[kCGWindowOwnerPID as String] as? pid_t) == pid {
            // Filter out non-visible windows
            if let layer = window[kCGWindowLayer as String] as? Int, layer == 0 {
                targetWindows.append(window)
            }
        }

        // Sort by window size (largest first) to find the main window
        targetWindows.sort { window1, window2 in
            let bounds1 = window1[kCGWindowBounds as String] as? [String: Any] ?? [:]
            let bounds2 = window2[kCGWindowBounds as String] as? [String: Any] ?? [:]

            let area1 = (bounds1["Width"] as? Double ?? 0) * (bounds1["Height"] as? Double ?? 0)
            let area2 = (bounds2["Width"] as? Double ?? 0) * (bounds2["Height"] as? Double ?? 0)

            return area1 > area2
        }

        // Return the title of the largest window (likely the main window)
        for window in targetWindows {
            if let title = window[kCGWindowName as String] as? String, !title.isEmpty {
                return title
            }
        }

        return nil
    }

    private func browserURL(for bundleID: String) -> URL? {
        // TODO: Implement browser URL capture without AppleScript to avoid Security framework issues
        // For now, return nil so we can test the basic heartbeat functionality
        return nil
    }
}