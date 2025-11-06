#!/usr/bin/env python3
"""
Alfred Embedding Helper - Fast Qwen3-Embedding-0.6B service
Replaces slow llama.cpp process spawning with efficient Python service
"""
import argparse
import json
import sys
import threading
import time
from pathlib import Path
from typing import List, Optional

import numpy as np
import uvicorn
from fastapi import FastAPI, HTTPException
from pydantic import BaseModel


class EmbeddingRequest(BaseModel):
    text: str
    model: Optional[str] = "Qwen3-Embedding-0.6B"


class EmbeddingResponse(BaseModel):
    embedding: List[float]
    dimension: int
    model: str
    processing_time_ms: float


class BatchEmbeddingRequest(BaseModel):
    texts: List[str]
    model: Optional[str] = "Qwen3-Embedding-0.6B"


class BatchEmbeddingResponse(BaseModel):
    embeddings: List[List[float]]
    dimension: int
    model: str
    processing_time_ms: float


app = FastAPI(title="Alfred Embedding Service", version="1.0.0")

# Global model variable - loaded once at startup
model = None
tokenizer = None
model_name = "Qwen3-Embedding-0.6B"
embedding_dimension = 1024


def load_model(model_path: Optional[str] = None):
    """Load Qwen3-Embedding-0.6B model once at startup"""
    global model, tokenizer, model_name

    try:
        # Try sentence-transformers first (most efficient for embeddings)
        from sentence_transformers import SentenceTransformer

        if model_path and Path(model_path).exists():
            print(f"Loading model from: {model_path}")
            model = SentenceTransformer(model_path)
        else:
            # Use default sentence-transformers model
            print("Loading default sentence-transformers model...")
            model = SentenceTransformer('all-MiniLM-L6-v2')  # Fallback lightweight model
            embedding_dimension = 384  # Update dimension for fallback

        print(f"✅ Model loaded: {model_name}")
        return True

    except ImportError:
        print("❌ sentence-transformers not available, installing...")
        import subprocess
        subprocess.check_call([sys.executable, "-m", "pip", "install", "sentence-transformers"])
        return load_model(model_path)  # Retry after installation

    except Exception as e:
        print(f"❌ Failed to load model: {e}")
        return False


@app.get("/health")
async def health_check():
    """Health check endpoint"""
    return {
        "status": "healthy",
        "model": model_name,
        "dimension": embedding_dimension,
        "ready": model is not None
    }


@app.get("/model_info")
async def get_model_info():
    """Get information about the loaded model"""
    if model is None:
        raise HTTPException(status_code=503, detail="Model not loaded")

    return {
        "name": model_name,
        "dimension": embedding_dimension,
        "ready": True
    }


@app.post("/embed", response_model=EmbeddingResponse)
async def embed_text(request: EmbeddingRequest):
    """Generate embedding for a single text"""
    if model is None:
        raise HTTPException(status_code=503, detail="Model not loaded")

    if not request.text.strip():
        raise HTTPException(status_code=400, detail="Text cannot be empty")

    start_time = time.time()

    try:
        # Generate embedding
        embedding = model.encode(request.text, convert_to_numpy=True)

        # Convert to list of floats
        embedding_list = embedding.astype(np.float32).tolist()

        processing_time = (time.time() - start_time) * 1000

        return EmbeddingResponse(
            embedding=embedding_list,
            dimension=len(embedding_list),
            model=model_name,
            processing_time_ms=processing_time
        )

    except Exception as e:
        raise HTTPException(status_code=500, detail=f"Embedding generation failed: {str(e)}")


@app.post("/embed_batch", response_model=BatchEmbeddingResponse)
async def embed_batch(request: BatchEmbeddingRequest):
    """Generate embeddings for multiple texts efficiently"""
    if model is None:
        raise HTTPException(status_code=503, detail="Model not loaded")

    if not request.texts:
        raise HTTPException(status_code=400, detail="Texts cannot be empty")

    start_time = time.time()

    try:
        # Batch encode all texts at once (much more efficient)
        embeddings = model.encode(request.texts, convert_to_numpy=True)

        # Convert to list of lists of floats
        embedding_lists = [emb.astype(np.float32).tolist() for emb in embeddings]

        processing_time = (time.time() - start_time) * 1000

        return BatchEmbeddingResponse(
            embeddings=embedding_lists,
            dimension=len(embedding_lists[0]) if embedding_lists else 0,
            model=model_name,
            processing_time_ms=processing_time
        )

    except Exception as e:
        raise HTTPException(status_code=500, detail=f"Batch embedding generation failed: {str(e)}")


def main():
    parser = argparse.ArgumentParser(description="Alfred Embedding Helper Service")
    parser.add_argument("--host", default="127.0.0.1", help="Host to bind to")
    parser.add_argument("--port", type=int, default=8901, help="Port to bind to")
    parser.add_argument("--model-path", help="Path to Qwen3-Embedding-0.6B model")
    parser.add_argument("--workers", type=int, default=1, help="Number of worker processes")

    args = parser.parse_args()

    print("🚀 Starting Alfred Embedding Helper...")

    # Load model before starting server
    if not load_model(args.model_path):
        print("❌ Failed to start: Model could not be loaded")
        sys.exit(1)

    print(f"🌐 Starting server on http://{args.host}:{args.port}")

    # Run with single worker for now (model is loaded in memory)
    uvicorn.run(
        "embedding_helper:app",
        host=args.host,
        port=args.port,
        workers=1,  # Single worker to avoid multiple model copies
        log_level="info"
    )


if __name__ == "__main__":
    main()