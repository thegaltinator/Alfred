from __future__ import annotations

import os
from pathlib import Path
from typing import Iterable, List

import numpy as np
from llama_cpp import Llama

MODEL_REL_PATH = "Models/Qwen3-Embedding-0.6B-f16.gguf"
MODELS_HOME = os.path.join(Path.home(), "Library", "Application Support", "Alfred")
DEFAULT_MODEL_PATH = os.path.join(MODELS_HOME, MODEL_REL_PATH)
EXPECTED_DIM = 1024


class QwenEmbeddingModel:
    """Wraps llama.cpp embedding-only inference for Qwen3-Embedding-0.6B."""

    def __init__(self, model_path: str | None = None):
        resolved = model_path or os.environ.get("QWEN_EMBED_MODEL", DEFAULT_MODEL_PATH)
        if not os.path.exists(resolved):
            raise FileNotFoundError(
                f"Qwen embedding model not found at {resolved}. Set QWEN_EMBED_MODEL or install the GGUF file."
            )
        self._model_path = resolved
        # Load once; reuse context (embedding=True keeps memory moderate)
        self._llama = Llama(
            model_path=resolved,
            embedding=True,
            n_gpu_layers=int(os.environ.get("QWEN_EMBED_N_GPU", "0")),
            n_ctx=2048,
            logits_all=False,
            score=False,
        )
        self._dimension = self._llama.metadata.get("embedding_length", EXPECTED_DIM)
        if self._dimension != EXPECTED_DIM:
            raise RuntimeError(
                f"Qwen embedding dim mismatch: expected {EXPECTED_DIM}, got {self._dimension}."
            )

    @property
    def dimension(self) -> int:
        return self._dimension

    def embed(self, texts: Iterable[str]) -> np.ndarray:
        payload: List[str] = [normalize_text(t) for t in texts if t.strip()]
        if not payload:
            return np.empty((0, self._dimension), dtype=np.float32)

        vectors = []
        for text in payload:
            result = self._llama.create_embedding(input=text)
            data = result["data"][0]["embedding"]
            vec = np.array(data, dtype=np.float32)
            if vec.shape[0] != self._dimension:
                raise RuntimeError(
                    f"Embedding dimension mismatch. Expected {self._dimension}, got {vec.shape[0]}"
                )
            norm = np.linalg.norm(vec)
            if norm == 0:
                raise RuntimeError("Zero-norm embedding encountered")
            vectors.append(vec / norm)
        return np.vstack(vectors)


def normalize_text(text: str) -> str:
    return " ".join(text.strip().split())
