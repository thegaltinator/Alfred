import Foundation
import SQLite3

public enum SQLiteStoreError: Error, LocalizedError {
    case openFailed(String)
    case executionFailed(String)
    case databaseUnavailable

    public var errorDescription: String? {
        switch self {
        case .openFailed(let message):
            return "Failed to open database: \(message)"
        case .executionFailed(let message):
            return "SQLite execution failed: \(message)"
        case .databaseUnavailable:
            return "Database is not ready"
        }
    }
}

public final class SQLiteStore {
    public static let shared = SQLiteStore()

    public let databaseURL: URL

    private let queue = DispatchQueue(label: "com.alfred.memory.sqlite")
    private var db: OpaquePointer?

    private init() {
        var resolvedURL = URL(fileURLWithPath: "/dev/null")
        do {
            resolvedURL = try Self.resolveDatabaseURL()
            let handle = try Self.openDatabase(at: resolvedURL)
            db = handle
            databaseURL = resolvedURL
            Self.enableWAL(on: handle)
            Self.createTables(on: handle)
            print("💾 SQLiteStore: Database opened at \(resolvedURL.path) with WAL mode")
        } catch {
            db = nil
            databaseURL = resolvedURL
            print("❌ SQLiteStore init error: \(error.localizedDescription)")
        }
    }

    deinit {
        if let handle = db {
            sqlite3_close(handle)
        }
    }

    public func addNote(content: String, metadata: String? = nil) throws -> Int64 {
        try queue.sync {
            guard let handle = db else { throw SQLiteStoreError.databaseUnavailable }
            let insertSQL = "INSERT INTO notes (uuid, text, content, metadata, ts) VALUES (?1, ?2, ?3, ?4, ?5);"
            var statement: OpaquePointer?
            guard sqlite3_prepare_v2(handle, insertSQL, -1, &statement, nil) == SQLITE_OK else {
                throw SQLiteStoreError.executionFailed(Self.currentErrorMessage(from: handle))
            }
            defer { sqlite3_finalize(statement) }

            let uuid = UUID().uuidString
            if sqlite3_bind_text(statement, 1, uuid, -1, SQLITE_TRANSIENT) != SQLITE_OK {
                throw SQLiteStoreError.executionFailed(Self.currentErrorMessage(from: handle))
            }

            if sqlite3_bind_text(statement, 2, content, -1, SQLITE_TRANSIENT) != SQLITE_OK {
                throw SQLiteStoreError.executionFailed(Self.currentErrorMessage(from: handle))
            }

            if sqlite3_bind_text(statement, 3, content, -1, SQLITE_TRANSIENT) != SQLITE_OK {
                throw SQLiteStoreError.executionFailed(Self.currentErrorMessage(from: handle))
            }

            if let metadata = metadata {
                if sqlite3_bind_text(statement, 4, metadata, -1, SQLITE_TRANSIENT) != SQLITE_OK {
                    throw SQLiteStoreError.executionFailed(Self.currentErrorMessage(from: handle))
                }
            } else if sqlite3_bind_null(statement, 4) != SQLITE_OK {
                throw SQLiteStoreError.executionFailed(Self.currentErrorMessage(from: handle))
            }

            let timestamp = Int32(Date().timeIntervalSince1970)
            if sqlite3_bind_int(statement, 5, timestamp) != SQLITE_OK {
                throw SQLiteStoreError.executionFailed(Self.currentErrorMessage(from: handle))
            }

            guard sqlite3_step(statement) == SQLITE_DONE else {
                throw SQLiteStoreError.executionFailed(Self.currentErrorMessage(from: handle))
            }

            return sqlite3_last_insert_rowid(handle)
        }
    }

    public func noteCount() throws -> Int {
        try queue.sync {
            guard let handle = db else { throw SQLiteStoreError.databaseUnavailable }
            let countSQL = "SELECT COUNT(*) FROM notes;"
            var statement: OpaquePointer?
            guard sqlite3_prepare_v2(handle, countSQL, -1, &statement, nil) == SQLITE_OK else {
                throw SQLiteStoreError.executionFailed(Self.currentErrorMessage(from: handle))
            }
            defer { sqlite3_finalize(statement) }

            guard sqlite3_step(statement) == SQLITE_ROW else {
                throw SQLiteStoreError.executionFailed(Self.currentErrorMessage(from: handle))
            }

            return Int(sqlite3_column_int(statement, 0))
        }
    }
}

// MARK: - Setup helpers

private extension SQLiteStore {
    static func resolveDatabaseURL() throws -> URL {
        let fm = FileManager.default
        guard let baseDir = fm.urls(for: .applicationSupportDirectory, in: .userDomainMask).first else {
            throw SQLiteStoreError.openFailed("Application Support directory missing")
        }
        let alfredDir = baseDir.appendingPathComponent("Alfred", isDirectory: true)
        if !fm.fileExists(atPath: alfredDir.path) {
            try fm.createDirectory(at: alfredDir, withIntermediateDirectories: true)
        }
        return alfredDir.appendingPathComponent("memory.db", isDirectory: false)
    }

    static func openDatabase(at url: URL) throws -> OpaquePointer? {
        var handle: OpaquePointer?
        let result = sqlite3_open(url.path, &handle)
        guard result == SQLITE_OK, let dbHandle = handle else {
            if let handle = handle {
                sqlite3_close(handle)
            }
            throw SQLiteStoreError.openFailed(String(cString: sqlite3_errmsg(handle)))
        }
        return dbHandle
    }

    static func enableWAL(on handle: OpaquePointer?) {
        guard let handle else { return }
        sqlite3_exec(handle, "PRAGMA journal_mode=WAL;", nil, nil, nil)
    }

    static func createTables(on handle: OpaquePointer?) {
        guard let handle else { return }
        let createSQL = """
        CREATE TABLE IF NOT EXISTS notes (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            uuid TEXT UNIQUE NOT NULL,
            text TEXT,
            content TEXT NOT NULL,
            ts INTEGER DEFAULT (strftime('%s','now')),
            created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
            updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
            metadata TEXT,
            is_deleted BOOLEAN DEFAULT 0
        );
        """
        if sqlite3_exec(handle, createSQL, nil, nil, nil) != SQLITE_OK {
            let message = String(cString: sqlite3_errmsg(handle))
            print("⚠️ SQLiteStore table creation failed: \(message)")
        } else {
            print("📝 SQLiteStore: Notes table ready")
        }
        Self.ensureColumnIfMissing(handle, column: "text", definition: "TEXT")
        Self.ensureColumnIfMissing(handle, column: "ts", definition: "INTEGER DEFAULT (strftime('%s','now'))")
        let indexSchema = """
        CREATE INDEX IF NOT EXISTS idx_notes_uuid ON notes(uuid);
        CREATE INDEX IF NOT EXISTS idx_notes_created_at ON notes(created_at);
        """
        if sqlite3_exec(handle, indexSchema, nil, nil, nil) != SQLITE_OK {
            let message = String(cString: sqlite3_errmsg(handle))
            print("⚠️ SQLiteStore index creation failed: \(message)")
        }
    }

    static func ensureColumnIfMissing(_ handle: OpaquePointer, column: String, definition: String) {
        var statement: OpaquePointer?
        if sqlite3_prepare_v2(handle, "PRAGMA table_info(notes);", -1, &statement, nil) == SQLITE_OK {
            var exists = false
            while sqlite3_step(statement) == SQLITE_ROW {
                if let name = sqlite3_column_text(statement, 1), String(cString: name) == column {
                    exists = true
                    break
                }
            }
            sqlite3_finalize(statement)
            if !exists {
                let alterSQL = "ALTER TABLE notes ADD COLUMN \(column) \(definition);"
                if sqlite3_exec(handle, alterSQL, nil, nil, nil) != SQLITE_OK {
                    print("⚠️ SQLiteStore: failed adding column \(column): \(currentErrorMessage(from: handle))")
                }
            }
        }
    }

    static func currentErrorMessage(from handle: OpaquePointer?) -> String {
        if let handle {
            return String(cString: sqlite3_errmsg(handle))
        }
        return "Unknown SQLite error"
    }
}

private let SQLITE_TRANSIENT = unsafeBitCast(-1, to: sqlite3_destructor_type.self)
