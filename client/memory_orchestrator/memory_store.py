"""
SQLite-based memory store with vector similarity search
Optimized for on-device memory storage and retrieval
"""

import json
import sqlite3
import uuid
from pathlib import Path
from typing import List, Dict, Optional, Any

import numpy as np


class MemoryStore:
    """Efficient SQLite memory store with vector similarity search"""

    def __init__(self, db_path: Path):
        self.db_path = db_path
        self._ensure_directory()
        self._init_database()

    def _ensure_directory(self):
        """Ensure database directory exists"""
        self.db_path.parent.mkdir(parents=True, exist_ok=True)

    def _init_database(self):
        """Initialize SQLite database with proper schema"""
        with sqlite3.connect(self.db_path) as conn:
            # Enable WAL mode for better concurrency
            conn.execute("PRAGMA journal_mode=WAL")

            # Create memories table
            conn.execute("""
                CREATE TABLE IF NOT EXISTS memories (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    uuid TEXT UNIQUE NOT NULL,
                    content TEXT NOT NULL,
                    metadata TEXT,
                    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
                    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
                    is_deleted INTEGER DEFAULT 0
                )
            """)

            # Create embeddings table
            conn.execute("""
                CREATE TABLE IF NOT EXISTS embeddings (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    memory_id INTEGER NOT NULL,
                    embedding BLOB NOT NULL,
                    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
                    FOREIGN KEY (memory_id) REFERENCES memories(id) ON DELETE CASCADE
                )
            """)

            # Create indexes for performance
            conn.execute("CREATE UNIQUE INDEX IF NOT EXISTS idx_memories_uuid ON memories(uuid)")
            conn.execute("CREATE UNIQUE INDEX IF NOT EXISTS idx_embeddings_memory_id ON embeddings(memory_id)")
            conn.execute("CREATE INDEX IF NOT EXISTS idx_memories_created_at ON memories(created_at)")
            conn.execute("CREATE INDEX IF NOT EXISTS idx_memories_is_deleted ON memories(is_deleted)")

            conn.commit()

    def add_memory(self, content: str, metadata: Dict[str, Any], embedding: np.ndarray) -> str:
        """
        Add a new memory with embedding

        Args:
            content: Memory content text
            metadata: Optional metadata dictionary
            embedding: Numpy array with embedding vector

        Returns:
            UUID of the created memory
        """
        memory_uuid = str(uuid.uuid4())
        metadata_json = json.dumps(metadata) if metadata else None
        embedding_blob = embedding.astype(np.float32).tobytes()

        with sqlite3.connect(self.db_path) as conn:
            # Add memory
            cursor = conn.execute("""
                INSERT INTO memories (uuid, content, metadata)
                VALUES (?, ?, ?)
            """, (memory_uuid, content, metadata_json))

            memory_id = cursor.lastrowid

            # Add embedding
            conn.execute("""
                INSERT INTO embeddings (memory_id, embedding)
                VALUES (?, ?)
            """, (memory_id, embedding_blob))

            conn.commit()

        return memory_uuid

    def get_memory(self, memory_uuid: str) -> Optional[Dict[str, Any]]:
        """
        Get a memory by UUID

        Args:
            memory_uuid: UUID of the memory

        Returns:
            Memory dictionary or None if not found
        """
        with sqlite3.connect(self.db_path) as conn:
            cursor = conn.execute("""
                SELECT uuid, content, metadata, created_at, updated_at
                FROM memories
                WHERE uuid = ? AND is_deleted = 0
            """, (memory_uuid,))

            row = cursor.fetchone()
            if row is None:
                return None

            return {
                'uuid': row[0],
                'content': row[1],
                'metadata': json.loads(row[2]) if row[2] else {},
                'created_at': row[3],
                'updated_at': row[4]
            }

    def search_similar(self, query_embedding: np.ndarray, limit: int = 5, threshold: float = 0.7) -> List[Dict[str, Any]]:
        """
        Search for similar memories using cosine similarity

        Args:
            query_embedding: Query embedding vector
            limit: Maximum number of results
            threshold: Minimum similarity threshold

        Returns:
            List of memory dictionaries with similarity scores
        """
        with sqlite3.connect(self.db_path) as conn:
            # Get all embeddings for active memories
            cursor = conn.execute("""
                SELECT m.id, m.uuid, m.content, m.metadata, m.created_at, e.embedding
                FROM memories m
                JOIN embeddings e ON m.id = e.memory_id
                WHERE m.is_deleted = 0
            """)

            results = []
            for memory_id, memory_uuid, content, metadata, created_at, embedding_blob in cursor.fetchall():
                # Convert blob back to numpy array
                stored_embedding = np.frombuffer(embedding_blob, dtype=np.float32)

                # Calculate cosine similarity
                similarity = self._cosine_similarity(query_embedding, stored_embedding)

                if similarity >= threshold:
                    results.append({
                        'id': memory_id,
                        'uuid': memory_uuid,
                        'content': content,
                        'metadata': json.loads(metadata) if metadata else {},
                        'created_at': created_at,
                        'similarity': float(similarity)
                    })

            # Sort by similarity and limit
            results.sort(key=lambda x: x['similarity'], reverse=True)
            return results[:limit]

    def list_memories(self, limit: int = 10, offset: int = 0) -> List[Dict[str, Any]]:
        """
        List recent memories

        Args:
            limit: Maximum number of memories to return
            offset: Number of memories to skip

        Returns:
            List of memory dictionaries
        """
        with sqlite3.connect(self.db_path) as conn:
            cursor = conn.execute("""
                SELECT uuid, content, metadata, created_at, updated_at
                FROM memories
                WHERE is_deleted = 0
                ORDER BY created_at DESC
                LIMIT ? OFFSET ?
            """, (limit, offset))

            memories = []
            for uuid, content, metadata, created_at, updated_at in cursor.fetchall():
                memories.append({
                    'uuid': uuid,
                    'content': content,
                    'metadata': json.loads(metadata) if metadata else {},
                    'created_at': created_at,
                    'updated_at': updated_at
                })

            return memories

    def delete_memory(self, memory_uuid: str) -> bool:
        """
        Soft delete a memory

        Args:
            memory_uuid: UUID of the memory to delete

        Returns:
            True if deleted, False if not found
        """
        with sqlite3.connect(self.db_path) as conn:
            cursor = conn.execute("""
                UPDATE memories
                SET is_deleted = 1, updated_at = CURRENT_TIMESTAMP
                WHERE uuid = ? AND is_deleted = 0
            """, (memory_uuid,))

            conn.commit()
            return cursor.rowcount > 0

    def get_stats(self) -> Dict[str, int]:
        """
        Get memory store statistics

        Returns:
            Dictionary with statistics
        """
        with sqlite3.connect(self.db_path) as conn:
            total_memories = conn.execute("""
                SELECT COUNT(*) FROM memories
            """).fetchone()[0]

            active_memories = conn.execute("""
                SELECT COUNT(*) FROM memories WHERE is_deleted = 0
            """).fetchone()[0]

            total_embeddings = conn.execute("""
                SELECT COUNT(*) FROM embeddings
            """).fetchone()[0]

            return {
                'total_memories': total_memories,
                'active_memories': active_memories,
                'deleted_memories': total_memories - active_memories,
                'total_embeddings': total_embeddings
            }

    def _cosine_similarity(self, a: np.ndarray, b: np.ndarray) -> float:
        """
        Calculate cosine similarity between two vectors

        Args:
            a: First vector
            b: Second vector

        Returns:
            Cosine similarity score (0.0 to 1.0)
        """
        dot_product = np.dot(a, b)
        norm_a = np.linalg.norm(a)
        norm_b = np.linalg.norm(b)

        if norm_a == 0 or norm_b == 0:
            return 0.0

        return float(dot_product / (norm_a * norm_b))

    def vacuum(self):
        """Vacuum the database to reclaim space"""
        with sqlite3.connect(self.db_path) as conn:
            conn.execute("VACUUM")
            conn.commit()

    def backup(self, backup_path: Path):
        """
        Create a backup of the database

        Args:
            backup_path: Path where to save the backup
        """
        backup_path.parent.mkdir(parents=True, exist_ok=True)

        with sqlite3.connect(self.db_path) as source:
            with sqlite3.connect(backup_path) as backup:
                source.backup(backup)


# Standalone test function
def test_memory_store():
    """Test the memory store functionality"""
    import tempfile
    import os

    # Create temporary database
    with tempfile.TemporaryDirectory() as temp_dir:
        db_path = Path(temp_dir) / "test_memory.db"
        store = MemoryStore(db_path)

        print("Testing memory store...")

        # Test adding memories
        uuid1 = store.add_memory(
            content="User prefers working in the morning",
            metadata={"type": "preference", "time": "morning"},
            embedding=np.random.randn(1024).astype(np.float32)
        )

        uuid2 = store.add_memory(
            content="User is learning Swift programming",
            metadata={"type": "learning", "topic": "swift"},
            embedding=np.random.randn(1024).astype(np.float32)
        )

        print(f"✅ Added memories: {uuid1}, {uuid2}")

        # Test retrieving memories
        memory1 = store.get_memory(uuid1)
        print(f"✅ Retrieved memory: {memory1['content'][:50]}...")

        # Test search (with random embeddings for testing)
        query_embedding = np.random.randn(1024).astype(np.float32)
        results = store.search_similar(query_embedding, limit=5, threshold=0.1)  # Low threshold for testing
        print(f"✅ Search returned {len(results)} results")

        # Test listing
        memories = store.list_memories(limit=10)
        print(f"✅ Listed {len(memories)} memories")

        # Test stats
        stats = store.get_stats()
        print(f"✅ Stats: {stats}")

        print("✅ Memory store test completed successfully")


if __name__ == "__main__":
    test_memory_store()