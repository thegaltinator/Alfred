"""
Alfred Memory Orchestrator - Simple on-device memory orchestration
Direct function calls for Swift integration, no HTTP overhead.
"""

import json
import sys
from pathlib import Path
from typing import List, Dict, Optional, Any

from .embeddings import EmbeddingService
from .memory_store import MemoryStore
from .cerebras import CerebrasClient


class MemoryOrchestrator:
    """Main orchestrator for memory, embeddings, and Cerebras integration"""

    def __init__(self, db_path: Optional[Path] = None, cerebras_api_key: Optional[str] = None):
        # Set default database path
        if db_path is None:
            db_path = Path.home() / "Library" / "Application Support" / "Alfred" / "memory.db"

        # Initialize components
        self.embeddings = EmbeddingService()
        self.memory_store = MemoryStore(db_path)
        self.cerebras = CerebrasClient(cerebras_api_key) if cerebras_api_key else None

    def process_transcript(self, transcript: str, max_memories: int = 5) -> Dict[str, Any]:
        """
        Process user transcript and return speech plan with relevant memories

        Args:
            transcript: User's spoken transcript
            max_memories: Maximum number of relevant memories to retrieve

        Returns:
            Dict with speech_plan, retrieved_memories, processing_time_ms
        """
        import time
        start_time = time.time()

        try:
            # Generate embedding for transcript
            query_embedding = self.embeddings.embed(transcript)

            # Search for relevant memories
            memories = self.memory_store.search_similar(
                query_embedding=query_embedding,
                limit=max_memories
            )

            # Generate response using Cerebras or fallback
            if self.cerebras:
                speech_plan = self.cerebras.chat_with_memory(transcript, memories)
            else:
                speech_plan = _fallback_response(transcript, memories)

            processing_time = (time.time() - start_time) * 1000

            return {
                "speech_plan": speech_plan,
                "retrieved_memories": memories,
                "processing_time_ms": processing_time
            }

        except Exception as e:
            return {
                "speech_plan": f"I'm having trouble processing that right now. You said: {transcript}",
                "retrieved_memories": [],
                "processing_time_ms": (time.time() - start_time) * 1000,
                "error": str(e)
            }

    def add_memory(self, content: str, metadata: Optional[Dict] = None) -> str:
        """
        Add a new memory to the store

        Args:
            content: Memory content
            metadata: Optional metadata dictionary

        Returns:
            UUID of the created memory
        """
        try:
            # Generate embedding for the memory
            embedding = self.embeddings.embed(content)

            # Store in memory database
            memory_uuid = self.memory_store.add_memory(
                content=content,
                metadata=metadata or {},
                embedding=embedding
            )

            return memory_uuid

        except Exception as e:
            raise Exception(f"Failed to add memory: {str(e)}")

    def search_memories(self, query: str, limit: int = 5) -> List[Dict]:
        """
        Search memories by semantic similarity

        Args:
            query: Search query
            limit: Maximum number of results

        Returns:
            List of memory dictionaries with similarity scores
        """
        try:
            query_embedding = self.embeddings.embed(query)
            return self.memory_store.search_similar(query_embedding, limit)

        except Exception as e:
            print(f"Search failed: {e}", file=sys.stderr)
            return []


def _fallback_response(transcript: str, memories: List[Dict]) -> str:
    """Generate fallback response when Cerebras is unavailable"""
    if memories:
        memory_summary = f"I found {len(memories)} relevant memories related to what you said."
        return f"I heard you say: {transcript}. {memory_summary}"
    else:
        return f"I heard you say: {transcript}"


# CLI interface for testing
def main():
    """Command line interface for testing the memory orchestrator"""
    if len(sys.argv) < 2:
        print("Usage: python -m memory_orchestrator <command> [args]")
        print("Commands:")
        print("  process <transcript>     - Process transcript and get speech plan")
        print("  add <content>            - Add new memory")
        print("  search <query>           - Search memories")
        print("  test                    - Run basic tests")
        sys.exit(1)

    command = sys.argv[1]

    # Initialize orchestrator
    import os
    api_key = os.getenv('CEREBRAS_API_KEY')
    orchestrator = MemoryOrchestrator(cerebras_api_key=api_key)

    try:
        if command == "process":
            if len(sys.argv) < 3:
                print("Usage: python -m memory_orchestrator process <transcript>")
                sys.exit(1)

            transcript = " ".join(sys.argv[2:])
            result = orchestrator.process_transcript(transcript)
            print(json.dumps(result, indent=2))

        elif command == "add":
            if len(sys.argv) < 3:
                print("Usage: python -m memory_orchestrator add <content>")
                sys.exit(1)

            content = " ".join(sys.argv[2:])
            memory_uuid = orchestrator.add_memory(content)
            print(f"Added memory: {memory_uuid}")

        elif command == "search":
            if len(sys.argv) < 3:
                print("Usage: python -m memory_orchestrator search <query>")
                sys.exit(1)

            query = " ".join(sys.argv[2:])
            memories = orchestrator.search_memories(query)
            print(json.dumps(memories, indent=2))

        elif command == "test":
            print("Running basic functionality test...")

            # Test adding memories
            uuid1 = orchestrator.add_memory("User prefers working in the morning")
            uuid2 = orchestrator.add_memory("User is learning Swift programming")
            print(f"Added test memories: {uuid1}, {uuid2}")

            # Test search
            results = orchestrator.search_memories("programming")
            print(f"Search results for 'programming': {len(results)} memories found")

            # Test transcript processing
            result = orchestrator.process_transcript("I'm having trouble with Swift concurrency")
            print(f"Processed transcript with {len(result['retrieved_memories'])} memories")

            print("✅ Basic test completed successfully")

        else:
            print(f"Unknown command: {command}")
            sys.exit(1)

    except Exception as e:
        print(f"Error: {e}", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()