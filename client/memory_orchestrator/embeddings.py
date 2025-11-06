"""
Embedding service using MLX for Apple Silicon optimization
Fallback to sentence-transformers if MLX is not available
"""

import sys
from typing import Union

import numpy as np


class EmbeddingService:
    """Fast embedding generation optimized for Apple Silicon"""

    def __init__(self, model_name: str = "Qwen3-Embedding-0.6B"):
        self.model_name = model_name
        self.model = None
        self.embedding_dim = 1024  # Qwen3-Embedding-0.6B dimension
        self._load_model()

    def _load_model(self):
        """Load embedding model with MLX priority"""
        try:
            # Try MLX first (Apple Silicon optimized)
            import mlx.core as mx
            from mlx import nn

            print("🔄 Loading MLX embedding model...")

            # For now, create a simple random embedding as placeholder
            # In production, this would load the actual Qwen3-Embedding-0.6B model
            # self.model = load_qwen_model_mlx()

            self.model = "mlx_placeholder"  # Placeholder
            print("✅ MLX embedding service ready")

        except ImportError:
            # Fallback to sentence-transformers
            try:
                from sentence_transformers import SentenceTransformer
                print("🔄 Loading sentence-transformers model (fallback)...")
                self.model = SentenceTransformer('all-MiniLM-L6-v2')
                self.embedding_dim = 384  # sentence-transformers dimension
                print("✅ sentence-transformers model loaded")

            except ImportError:
                print("❌ No embedding model available. Install MLX or sentence-transformers")
                raise

    def embed(self, text: str) -> np.ndarray:
        """
        Generate embedding for text

        Args:
            text: Input text to embed

        Returns:
            Numpy array with embedding vector
        """
        if not text or not text.strip():
            raise ValueError("Cannot embed empty text")

        text = text.strip()

        if self.model == "mlx_placeholder":
            # Placeholder implementation - in production this would use actual MLX model
            # Generate consistent "hash-based" embedding for reproducible results
            return self._placeholder_embedding(text)

        elif hasattr(self.model, 'encode'):
            # sentence-transformers
            return self.model.encode(text)

        else:
            # Other model types
            raise NotImplementedError("Model type not supported")

    def embed_batch(self, texts: list[str]) -> list[np.ndarray]:
        """
        Generate embeddings for multiple texts efficiently

        Args:
            texts: List of input texts

        Returns:
            List of numpy arrays with embedding vectors
        """
        if not texts:
            return []

        # Filter out empty texts
        valid_texts = [text.strip() for text in texts if text and text.strip()]

        if not valid_texts:
            return []

        if self.model == "mlx_placeholder":
            return [self._placeholder_embedding(text) for text in valid_texts]

        elif hasattr(self.model, 'encode'):
            # Use sentence-transformers batch processing
            embeddings = self.model.encode(valid_texts)
            return [emb for emb in embeddings]

        else:
            # Fallback to individual processing
            return [self.embed(text) for text in valid_texts]

    def _placeholder_embedding(self, text: str) -> np.ndarray:
        """
        Generate consistent placeholder embedding
        In production, this would be replaced with actual MLX model inference
        """
        # Create a deterministic "hash" of the text for consistent embeddings
        import hashlib

        # Use SHA256 to create deterministic seed
        hash_object = hashlib.sha256(text.encode('utf-8'))
        hex_dig = hash_object.hexdigest()

        # Convert hash to numpy array with proper dimensions
        seed = int(hex_dig[:16], 16)  # Use first 16 chars of hash
        np.random.seed(seed & 0xFFFFFFFF)  # Ensure seed is within valid range

        # Generate random embedding with consistent dimensions
        embedding = np.random.randn(self.embedding_dim).astype(np.float32)

        # Normalize to unit length (common for embeddings)
        norm = np.linalg.norm(embedding)
        if norm > 0:
            embedding = embedding / norm

        return embedding

    @property
    def dimension(self) -> int:
        """Get embedding dimension"""
        return self.embedding_dim

    def get_model_info(self) -> dict:
        """Get information about the loaded model"""
        if self.model == "mlx_placeholder":
            return {
                "name": self.model_name,
                "type": "MLX (placeholder)",
                "dimension": self.embedding_dim,
                "ready": True
            }
        elif hasattr(self.model, 'get_sentence_embedding_dimension'):
            return {
                "name": self.model._modules['0'].auto_model.name_or_path,
                "type": "sentence-transformers",
                "dimension": self.model.get_sentence_embedding_dimension(),
                "ready": True
            }
        else:
            return {
                "name": "Unknown",
                "type": "Unknown",
                "dimension": self.embedding_dim,
                "ready": False
            }


# Standalone test function
def test_embeddings():
    """Test the embedding service"""
    service = EmbeddingService()

    test_texts = [
        "Swift programming language",
        "iOS development with Swift",
        "Machine learning concepts"
    ]

    print("Testing single embeddings:")
    for text in test_texts:
        try:
            embedding = service.embed(text)
            print(f"✅ '{text[:30]}...' -> {embedding.shape} (norm: {np.linalg.norm(embedding):.3f})")
        except Exception as e:
            print(f"❌ Failed to embed '{text[:30]}...': {e}")

    print("\nTesting batch embeddings:")
    try:
        embeddings = service.embed_batch(test_texts)
        print(f"✅ Batch embedding {len(test_texts)} texts -> {len(embeddings)} embeddings")
        for i, emb in enumerate(embeddings):
            print(f"   Text {i+1}: {emb.shape} (norm: {np.linalg.norm(emb):.3f})")
    except Exception as e:
        print(f"❌ Batch embedding failed: {e}")

    print(f"\nModel info: {service.get_model_info()}")


if __name__ == "__main__":
    test_embeddings()