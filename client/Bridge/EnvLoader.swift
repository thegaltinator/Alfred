import Foundation

final class EnvLoader {
    static let shared = EnvLoader()

    private var loaded = false
    private init() {}

    func loadEnvIfNeeded() {
        guard !loaded else { return }
        loaded = true

        let candidatePaths = Self.envPaths()
        for path in candidatePaths {
            if FileManager.default.fileExists(atPath: path) {
                loadEnvFile(at: path)
                break
            }
        }
    }

    private func loadEnvFile(at path: String) {
        guard let data = FileManager.default.contents(atPath: path),
              let contents = String(data: data, encoding: .utf8) else {
            return
        }

        contents
            .split(whereSeparator: \.isNewline)
            .forEach { rawLine in
                var line = String(rawLine)
                if let commentRange = line.range(of: "#") {
                    line.removeSubrange(commentRange.lowerBound..<line.endIndex)
                }
                guard let separatorIndex = line.firstIndex(of: "=") else { return }
                let key = String(line[..<separatorIndex]).trimmingCharacters(in: .whitespacesAndNewlines)
                let valueStart = line.index(after: separatorIndex)
                let value = String(line[valueStart...]).trimmingCharacters(in: .whitespacesAndNewlines)
                guard !key.isEmpty else { return }
                setenv(key, value, 1)
            }

        print("🔐 Loaded environment variables from \(path)")
    }

    private static func envPaths() -> [String] {
        var paths: [String] = []

        let fm = FileManager.default
        let cwd = fm.currentDirectoryPath
        paths.append(cwd + "/client/.env")
        paths.append(cwd + "/.env")

        if let bundlePath = Bundle.main.resourcePath {
            paths.append(bundlePath + "/.env")
        }

        let home = fm.homeDirectoryForCurrentUser.path
        paths.append(home + "/.alfred/.env")

        if let execPath = Bundle.main.bundleURL.deletingLastPathComponent().path as String? {
            paths.append(execPath + "/.env")
        }

        return Array(Set(paths))
    }
}
