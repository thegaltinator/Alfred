from __future__ import annotations

import argparse
import json
from pathlib import Path

import numpy as np

from core.index import MemoryIndex
from core.store import (
    backfill_embeddings,
    ensure_schema,
    fetch_notes_by_ids,
    get_connection,
    load_all_embeddings,
)

APP_SUPPORT = Path.home() / "Library" / "Application Support" / "Alfred"
INDEX_PATH = APP_SUPPORT / "index_flatip.faiss"


def cmd_migrate(args):
    conn = get_connection()
    ensure_schema(conn)
    print("✅ Migration complete (notes/note_embeddings ensured, WAL enabled)")


def cmd_backfill(args):
    conn = get_connection()
    ensure_schema(conn)
    from core.embeddings import QwenEmbeddingModel

    embedder = QwenEmbeddingModel()
    inserted = backfill_embeddings(conn, embedder, batch_size=args.batch)
    print(f"✅ Backfill complete: {inserted} embeddings upserted")


def cmd_build_index(args):
    conn = get_connection()
    ensure_schema(conn)
    ids, vectors = load_all_embeddings(conn)
    if vectors.size == 0:
        raise SystemExit("No embeddings available. Run backfill first.")
    index = MemoryIndex(vectors.shape[1], index_path=str(INDEX_PATH))
    index.build(vectors, ids)
    print(f"✅ Index built with {len(ids)} vectors at {INDEX_PATH}")


def cmd_nn(args):
    conn = get_connection()
    ids, vectors = load_all_embeddings(conn)
    if vectors.size == 0:
        raise SystemExit("Index empty. Build index first.")
    index = MemoryIndex(vectors.shape[1], index_path=str(INDEX_PATH))
    if INDEX_PATH.exists():
        index.load()
    else:
        index.build(vectors, ids)
    note_id = args.note_id
    match = [i for i, nid in enumerate(ids) if nid == note_id]
    if not match:
        raise SystemExit(f"note_id {note_id} missing in embeddings")
    vec = vectors[match[0]]
    neighbors = index.search(vec, top_k=args.top_k, exclude_id=note_id, min_score=args.min_score)
    enriched = []
    for nid, score in neighbors:
        note_rows = fetch_notes_by_ids(conn, [nid])
        content = note_rows[0]["text"] if note_rows else ""
        enriched.append({"note_id": nid, "score": score, "text": content})
    print(json.dumps(enriched, indent=2))


def main():
    parser = argparse.ArgumentParser(description="Alfred Python helper CLI")
    sub = parser.add_subparsers(dest="cmd", required=True)

    migrate = sub.add_parser("migrate", help="Ensure SQLite schema (notes + embeddings)")
    migrate.set_defaults(func=cmd_migrate)

    backfill = sub.add_parser("backfill", help="Backfill embeddings for existing notes")
    backfill.add_argument("--batch", type=int, default=16)
    backfill.set_defaults(func=cmd_backfill)

    build_index = sub.add_parser("build-index", help="Build FAISS index from embeddings")
    build_index.set_defaults(func=cmd_build_index)

    nn = sub.add_parser("nn", help="Nearest neighbors for note_id")
    nn.add_argument("note_id", type=int)
    nn.add_argument("--top-k", type=int, default=5)
    nn.add_argument("--min-score", type=float, default=0.0)
    nn.set_defaults(func=cmd_nn)

    args = parser.parse_args()
    args.func(args)


if __name__ == "__main__":
    main()
