import AppKit
import Foundation
import ApplicationServices

struct ActiveTabSnapshot {
    let title: String?
    let url: String?

    var domain: String? {
        guard let urlString = url, let url = URL(string: urlString), let host = url.host else {
            return nil
        }
        return host.lowercased()
    }
}

final class ActiveTabReader {
    private var automationAuthorizedApps: Set<String> = []
    private var automationDeniedApps: Set<String> = []
    private var compiledScripts: [String: NSAppleScript] = [:]

    func snapshot(for bundleIdentifier: String?) -> ActiveTabSnapshot? {
        guard let bundleIdentifier else { return nil }

        let script: String
        let targetAppName: String
        switch bundleIdentifier {
        case "com.apple.Safari":
            targetAppName = "Safari"
            script = """
            tell application "Safari"
                if (count of windows) is 0 then
                    return ""
                end if
                set theWindow to front window
                if (count of tabs of theWindow) is 0 then
                    return ""
                end if
                set theTab to current tab of theWindow
                set theTitle to name of theTab
                set theURL to URL of theTab
                return theTitle & "||" & theURL
            end tell
            """

        case "com.google.Chrome",
             "com.google.Chrome.canary",
             "com.brave.Browser",
             "company.thebrowser.Browser",
             "com.vivaldi.Vivaldi",
             "org.mozilla.firefox",
             "org.mozilla.firefoxdeveloperedition":

            let appName: String
            switch bundleIdentifier {
            case "company.thebrowser.Browser":
                appName = "Arc"
            case "com.brave.Browser":
                appName = "Brave Browser"
            case "com.vivaldi.Vivaldi":
                appName = "Vivaldi"
            default:
                appName = NSWorkspace.shared.urlForApplication(withBundleIdentifier: bundleIdentifier)?
                    .lastPathComponent
                    .replacingOccurrences(of: ".app", with: "") ?? "Google Chrome"
            }
            targetAppName = appName

            script = """
            tell application "\(appName)"
                if (count of windows) is 0 then
                    return ""
                end if
                set theWindow to front window
                set theTab to active tab of theWindow
                set theTitle to title of theTab
                set theURL to URL of theTab
                return theTitle & "||" & theURL
            end tell
            """

        default:
            return nil
        }

        let permissionGranted = ensureAutomationPermission(for: bundleIdentifier, appName: targetAppName)
        if !permissionGranted {
            print("[ActiveTabReader] Automation permission pending for \(targetAppName). Will still attempt AppleScript to trigger system prompt if needed.")
        }

        if let snapshot = executeAppleScript(script, cacheKey: bundleIdentifier) {
            return snapshot
        }
        print("[ActiveTabReader] Falling back to /usr/bin/osascript for \(targetAppName)")
        return executeOSScript(script)
    }

    private func ensureAutomationPermission(for bundleIdentifier: String, appName: String) -> Bool {
        if automationAuthorizedApps.contains(bundleIdentifier) {
            return true
        }

        let descriptor = NSAppleEventDescriptor(bundleIdentifier: bundleIdentifier)
        let status = AEDeterminePermissionToAutomateTarget(
            descriptor.aeDesc,
            typeWildCard,
            typeWildCard,
            true
        )

        switch status {
        case noErr:
            automationAuthorizedApps.insert(bundleIdentifier)
            automationDeniedApps.remove(bundleIdentifier)
            print("[ActiveTabReader] Automation permission granted for \(appName) (\(bundleIdentifier))")
            return true
        case OSStatus(errAEEventNotPermitted):
            if !automationDeniedApps.contains(bundleIdentifier) {
                automationDeniedApps.insert(bundleIdentifier)
                print("[ActiveTabReader] Automation permission denied for \(appName). Please enable Jarvis under System Settings → Privacy & Security → Automation.")
            }
            return false
        default:
            print("[ActiveTabReader] Automation permission check returned status \(status) for \(appName)")
            return false
        }
    }

    private func executeAppleScript(_ script: String, cacheKey: String) -> ActiveTabSnapshot? {
        guard let appleScript = cachedAppleScript(for: cacheKey, source: script) else {
            return nil
        }

        var errorInfo: NSDictionary?
        let descriptor = appleScript.executeAndReturnError(&errorInfo)

        if errorInfo == nil {
            if let output = descriptor.stringValue?.trimmingCharacters(in: .whitespacesAndNewlines),
               !output.isEmpty {
                print("[ActiveTabReader] NSAppleScript succeeded: \(output.prefix(100))")
                return parseOutput(output)
            }
            print("[ActiveTabReader] NSAppleScript returned empty output")
            return nil
        }

        if let errorInfo {
            let number = errorInfo[NSAppleScript.errorNumber] as? Int ?? 0
            let message = errorInfo[NSAppleScript.errorMessage] as? String ?? "unknown error"
            let brief = errorInfo[NSAppleScript.errorBriefMessage] as? String ?? "no detail"
            print("[ActiveTabReader] NSAppleScript error \(number): \(message) (\(brief))")

            if number == Int(errAEEventNotPermitted) {
                print("[ActiveTabReader] NSAppleScript denied automation access. Check System Settings → Privacy & Security → Automation.")
                if !automationDeniedApps.contains(cacheKey) {
                    automationDeniedApps.insert(cacheKey)
                }
            }
        } else {
            print("[ActiveTabReader] NSAppleScript failed without error info")
        }
        return nil
    }

    private func cachedAppleScript(for cacheKey: String, source: String) -> NSAppleScript? {
        if let cached = compiledScripts[cacheKey] {
            return cached
        }
        guard let script = NSAppleScript(source: source) else {
            print("[ActiveTabReader] Failed to compile AppleScript for \(cacheKey)")
            return nil
        }
        compiledScripts[cacheKey] = script
        return script
    }

    private func executeOSScript(_ script: String) -> ActiveTabSnapshot? {
        let task = Process()
        let pipe = Pipe()

        task.executableURL = URL(fileURLWithPath: "/usr/bin/osascript")
        task.arguments = ["-e", script]
        task.standardOutput = pipe
        task.standardError = pipe

        do {
            try task.run()
            task.waitUntilExit()

            let data = pipe.fileHandleForReading.readDataToEndOfFile()
            let output = String(data: data, encoding: .utf8)?.trimmingCharacters(in: .whitespacesAndNewlines)

            if task.terminationStatus == 0, let output = output, !output.isEmpty {
                print("[ActiveTabReader] osascript succeeded: \(output.prefix(100))")
                return parseOutput(output)
            } else {
                print("[ActiveTabReader] osascript failed with status: \(task.terminationStatus), output: \(output ?? "none")")
                return nil
            }
        } catch {
            print("[ActiveTabReader] osascript execution failed: \(error)")
            return nil
        }
    }

    private func parseOutput(_ output: String?) -> ActiveTabSnapshot? {
        guard let raw = output, !raw.isEmpty else {
            print("[ActiveTabReader] No output to parse")
            return nil
        }

        let parts = raw.components(separatedBy: "||")
        guard parts.count == 2 else {
            print("[ActiveTabReader] Invalid output format, parts count: \(parts.count), raw: \(raw.prefix(100))")
            return nil
        }

        let title = parts[0].trimmingCharacters(in: .whitespacesAndNewlines)
        let url = parts[1].trimmingCharacters(in: .whitespacesAndNewlines)

        if title.isEmpty && url.isEmpty {
            print("[ActiveTabReader] Both title and URL are empty")
            return nil
        }

        let result = ActiveTabSnapshot(title: title.isEmpty ? nil : title, url: url.isEmpty ? nil : url)
        print("[ActiveTabReader] Result - title: \(result.title?.prefix(50) ?? "nil"), url: \(result.url?.prefix(50) ?? "nil"), domain: \(result.domain ?? "nil")")
        return result
    }
}
