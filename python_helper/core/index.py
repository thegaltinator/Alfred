from __future__ import annotations

import os
from pathlib import Path
from typing import List, Sequence, Tuple

import faiss
import numpy as np

INDEX_FILE = os.path.join(Path.home(), "Library", "Application Support", "Alfred", "index_flatip.faiss")


class MemoryIndex:
    def __init__(self, dimension: int, index_path: str | None = None):
        self.dimension = dimension
        self.index_path = index_path or INDEX_FILE
        self.index: faiss.IndexIDMap2 | None = None

    def build(self, vectors: np.ndarray, ids: np.ndarray) -> None:
        self.index = faiss.IndexIDMap2(faiss.IndexFlatIP(self.dimension))
        if vectors.size == 0:
            return
        assert vectors.shape[0] == ids.shape[0]
        self.index.add_with_ids(vectors.astype(np.float32), ids.astype(np.int64))
        self.persist()

    def persist(self) -> None:
        if self.index is None:
            return
        os.makedirs(os.path.dirname(self.index_path), exist_ok=True)
        faiss.write_index(self.index, self.index_path)

    def load(self) -> None:
        if not os.path.exists(self.index_path):
            return
        self.index = faiss.read_index(self.index_path)
        if not isinstance(self.index, faiss.IndexIDMap2):
            # Wrap to ensure ID support
            base = self.index
            mapper = faiss.IndexIDMap2(faiss.IndexFlatIP(base.d))
            xb = base.reconstruct_n(0, base.ntotal)
            ids = np.arange(base.ntotal)
            mapper.add_with_ids(xb, ids)
            self.index = mapper

    def search(
        self,
        query_vector: np.ndarray,
        top_k: int,
        exclude_id: int | None = None,
        min_score: float = 0.0,
    ) -> List[Tuple[int, float]]:
        if self.index is None or self.index.ntotal == 0:
            return []
        q = np.asarray(query_vector, dtype=np.float32)
        if q.ndim == 1:
            q = q.reshape(1, -1)
        scores, ids = self.index.search(q, top_k + (1 if exclude_id is not None else 0))
        results: List[Tuple[int, float]] = []
        for score, idx in zip(scores[0], ids[0]):
            if idx == -1:
                continue
            if exclude_id is not None and idx == exclude_id:
                continue
            if score < min_score:
                continue
            results.append((int(idx), float(score)))
            if len(results) >= top_k:
                break
        return results
