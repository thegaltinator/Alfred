from __future__ import annotations

import os
import sqlite3
import time
from pathlib import Path
from typing import Iterable, List, Sequence, Tuple

import numpy as np

DB_PATH = os.path.join(Path.home(), "Library", "Application Support", "Alfred", "memory.db")
NOTE_EMBEDDINGS_TABLE = "note_embeddings"


def get_connection(db_path: str | None = None) -> sqlite3.Connection:
    path = db_path or DB_PATH
    conn = sqlite3.connect(path)
    conn.row_factory = sqlite3.Row
    conn.execute("PRAGMA journal_mode=WAL;")
    return conn


def ensure_schema(conn: sqlite3.Connection) -> None:
    conn.execute(
        """
        CREATE TABLE IF NOT EXISTS notes (
            id INTEGER PRIMARY KEY,
            text TEXT,
            ts INTEGER,
            content TEXT,
            uuid TEXT,
            created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
            updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
            metadata TEXT,
            is_deleted BOOLEAN DEFAULT 0
        );
        """
    )
    # Add missing columns if the legacy schema is present.
    _ensure_column(conn, "notes", "text", "TEXT")
    _ensure_column(conn, "notes", "ts", "INTEGER")

    conn.execute(
        """
        CREATE TABLE IF NOT EXISTS note_embeddings (
            note_id INTEGER PRIMARY KEY,
            dim INTEGER NOT NULL,
            vec BLOB NOT NULL,
            ts INTEGER NOT NULL,
            FOREIGN KEY(note_id) REFERENCES notes(id) ON DELETE CASCADE
        );
        """
    )
    conn.execute(
        """
        CREATE TRIGGER IF NOT EXISTS note_embeddings_cleanup
        AFTER DELETE ON notes
        FOR EACH ROW BEGIN
            DELETE FROM note_embeddings WHERE note_id = OLD.id;
        END;
        """
    )
    conn.commit()


def _ensure_column(conn: sqlite3.Connection, table: str, column: str, decl: str) -> None:
    cur = conn.execute(f"PRAGMA table_info({table});")
    names = {row[1] for row in cur.fetchall()}
    if column not in names:
        conn.execute(f"ALTER TABLE {table} ADD COLUMN {column} {decl};")
        conn.commit()


def fetch_notes_missing_embeddings(conn: sqlite3.Connection, limit: int = 64) -> List[sqlite3.Row]:
    query = f"""
        SELECT n.id, COALESCE(n.text, n.content, '') AS text,
               COALESCE(n.ts, CAST(strftime('%s', n.created_at) AS INTEGER), CAST(strftime('%s','now') AS INTEGER)) AS ts
        FROM notes n
        LEFT JOIN {NOTE_EMBEDDINGS_TABLE} e ON e.note_id = n.id
        WHERE e.note_id IS NULL AND LENGTH(COALESCE(n.text, n.content, '')) > 0 AND n.is_deleted = 0
        ORDER BY n.ts DESC
        LIMIT ?;
    """
    return list(conn.execute(query, (limit,)))


def upsert_embeddings(
    conn: sqlite3.Connection,
    rows: Sequence[Tuple[int, np.ndarray, int]],
    dim: int,
) -> None:
    payload = [
        (
            note_id,
            dim,
            memoryview(vec.astype(np.float32).tobytes()),
            ts,
        )
        for note_id, vec, ts in rows
    ]
    conn.executemany(
        f"""
        INSERT INTO {NOTE_EMBEDDINGS_TABLE}(note_id, dim, vec, ts)
        VALUES (?, ?, ?, ?)
        ON CONFLICT(note_id) DO UPDATE SET dim=excluded.dim, vec=excluded.vec, ts=excluded.ts;
        """,
        payload,
    )
    conn.commit()


def load_all_embeddings(conn: sqlite3.Connection) -> Tuple[np.ndarray, np.ndarray]:
    cur = conn.execute(
        f"SELECT note_id, dim, vec FROM {NOTE_EMBEDDINGS_TABLE} ORDER BY ts DESC;"
    )
    ids: List[int] = []
    vectors: List[np.ndarray] = []
    for row in cur:
        vec = np.frombuffer(row["vec"], dtype=np.float32)
        if vec.size != row["dim"]:
            continue
        ids.append(row["note_id"])
        vectors.append(vec)
    if not ids:
        return np.array([], dtype=np.int64), np.empty((0, 0), dtype=np.float32)
    return np.array(ids, dtype=np.int64), np.vstack(vectors)


def fetch_notes_by_ids(conn: sqlite3.Connection, note_ids: Iterable[int]) -> List[sqlite3.Row]:
    ids = list(note_ids)
    if not ids:
        return []
    placeholders = ",".join(["?"] * len(ids))
    query = f"""
        SELECT id,
               COALESCE(text, content, '') AS text,
               COALESCE(ts, CAST(strftime('%s', created_at) AS INTEGER), 0) AS ts
        FROM notes
        WHERE id IN ({placeholders}) AND is_deleted = 0;
    """
    return list(conn.execute(query, ids))


def fetch_recent_notes(conn: sqlite3.Connection, limit: int = 1000) -> List[sqlite3.Row]:
    query = """
        SELECT id,
               COALESCE(text, content, '') AS text,
               COALESCE(ts, CAST(strftime('%s', created_at) AS INTEGER), 0) AS ts
        FROM notes
        WHERE is_deleted = 0 AND LENGTH(COALESCE(text, content, '')) > 0
        ORDER BY ts DESC
        LIMIT ?;
    """
    return list(conn.execute(query, (limit,)))


def backfill_embeddings(conn: sqlite3.Connection, embedder, batch_size: int = 16) -> int:
    total = 0
    while True:
        rows = fetch_notes_missing_embeddings(conn, limit=batch_size)
        if not rows:
            break
        vectors = embedder.embed(row["text"] for row in rows)
        payload = [
            (row["id"], vec, row["ts"] or int(time.time()))
            for row, vec in zip(rows, vectors)
        ]
        upsert_embeddings(conn, payload, embedder.dimension)
        total += len(rows)
    return total
