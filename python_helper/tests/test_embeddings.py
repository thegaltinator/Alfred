import os
import numpy as np
import pytest

from core.embeddings import DEFAULT_MODEL_PATH, EXPECTED_DIM, QwenEmbeddingModel

model_exists = os.path.exists(DEFAULT_MODEL_PATH)


@pytest.mark.skipif(not model_exists, reason="Qwen embedding model not installed")
def test_qwen_embedding_normalized():
    embedder = QwenEmbeddingModel()
    vecs = embedder.embed(["test embedding vector"])
    assert vecs.shape == (1, EXPECTED_DIM)
    norm = np.linalg.norm(vecs[0])
    assert np.isclose(norm, 1.0, atol=1e-3)
