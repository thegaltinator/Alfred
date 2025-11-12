import numpy as np

from core.index import MemoryIndex


def test_memory_index_excludes_self(tmp_path):
    dim = 4
    vectors = np.array(
        [
            np.array([1, 0, 0, 0], dtype=np.float32),
            np.array([0.9, 0.1, 0, 0], dtype=np.float32),
            np.array([0, 1, 0, 0], dtype=np.float32),
        ]
    )
    ids = np.array([10, 11, 12], dtype=np.int64)
    index_path = tmp_path / "index.faiss"
    idx = MemoryIndex(dim, index_path=str(index_path))
    idx.build(vectors, ids)
    results = idx.search(vectors[0], top_k=2, exclude_id=10)
    assert results[0][0] == 11
    assert all(r[0] != 10 for r in results)
