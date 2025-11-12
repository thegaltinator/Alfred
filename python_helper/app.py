from __future__ import annotations

import json
import os
import sys
import time
from pathlib import Path
from typing import List

from dotenv import load_dotenv

from schema import ChatRequest, ChatResponse, MemoryRef, TokenUsage
from core.embeddings import QwenEmbeddingModel
from core.store import (
    ensure_schema,
    fetch_notes_by_ids,
    get_connection,
    load_all_embeddings,
)
from core.index import MemoryIndex
from core.prompt_builder import MemorySnippet, PromptBuilder
from core.cerberas import CerberasClient

ROOT = Path(__file__).resolve().parent.parent
APP_SUPPORT = Path.home() / "Library" / "Application Support" / "Alfred"
INDEX_PATH = APP_SUPPORT / "index_flatip.faiss"


def load_env() -> None:
    for env_path in (ROOT / ".env", ROOT / "client" / ".env"):
        if env_path.exists():
            load_dotenv(dotenv_path=str(env_path), override=False)


class Resources:
    def __init__(self):
        self.conn = get_connection()
        ensure_schema(self.conn)
        self.embedder = QwenEmbeddingModel()
        self.prompt_builder = PromptBuilder()
        self.index = MemoryIndex(self.embedder.dimension, index_path=str(INDEX_PATH))
        if INDEX_PATH.exists():
            self.index.load()
        else:
            self.refresh_index()
        self.cerberas = CerberasClient()

    def refresh_index(self) -> None:
        ids, vectors = load_all_embeddings(self.conn)
        if vectors.size == 0:
            self.index.index = None
            return
        self.index.build(vectors, ids)

    def handle_chat(self, req: ChatRequest) -> ChatResponse:
        if not req.user_text.strip():
            raise ValueError("user_text required")

        start = time.perf_counter()
        query_vecs = self.embedder.embed([req.user_text])
        if query_vecs.size == 0:
            raise RuntimeError("Failed to embed user_text")
        query = query_vecs[0]

        matches = self.index.search(
            query_vector=query,
            top_k=req.opts.top_k,
            exclude_id=None,
            min_score=req.opts.min_score,
        )
        used_memory_refs = [MemoryRef(id=note_id, score=score) for note_id, score in matches]

        rows = fetch_notes_by_ids(self.conn, [ref.id for ref in used_memory_refs])
        rows_by_id = {row["id"]: row for row in rows}
        snippets: List[MemorySnippet] = []
        for ref in used_memory_refs:
            row = rows_by_id.get(ref.id)
            if not row:
                continue
            snippets.append(
                MemorySnippet(
                    note_id=row["id"],
                    text=row["text"],
                    score=ref.score,
                    ts=row["ts"],
                )
            )

        prompt = self.prompt_builder.build(req.user_text, snippets)
        completion, token_usage = self.cerberas.complete(
            prompt,
            temperature=req.opts.temperature,
            stream=req.opts.stream,
        )
        latency_ms = int((time.perf_counter() - start) * 1000)
        return ChatResponse(
            assistant_text=completion,
            used_memory=used_memory_refs,
            latency_ms=latency_ms,
            token_usage=TokenUsage(**token_usage),
        )


def emit_response(payload: dict | str) -> None:
    if isinstance(payload, str):
        data = payload
    else:
        data = json.dumps(payload)
    sys.stdout.write(f"{data}\n")
    sys.stdout.flush()


def main() -> None:
    load_env()
    resources = Resources()
    print("🧠 python_helper ready", file=sys.stderr)
    for raw in sys.stdin:
        line = raw.strip()
        if not line:
            continue
        try:
            req = ChatRequest.model_validate_json(line)
        except Exception as exc:  # pylint: disable=broad-except
            emit_response({"error": f"invalid_request: {exc}"})
            continue
        try:
            resp = resources.handle_chat(req)
        except Exception as exc:  # pylint: disable=broad-except
            emit_response({"error": f"helper_failure: {exc}"})
            continue
        emit_response(resp.model_dump())


if __name__ == "__main__":
    main()
