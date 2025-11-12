from __future__ import annotations

from pydantic import BaseModel, Field
from typing import List, Optional


class ChatOptions(BaseModel):
    top_k: int = Field(default=8, ge=1, le=32)
    min_score: float = Field(default=0.28, ge=0.0, le=1.0)
    temperature: float = Field(default=0.4, ge=0.0, le=2.0)
    stream: bool = True


class ChatRequest(BaseModel):
    session_id: str
    user_text: str
    opts: ChatOptions = Field(default_factory=ChatOptions)


class MemoryRef(BaseModel):
    id: int
    score: float


class TokenUsage(BaseModel):
    prompt: int = 0
    completion: int = 0


class ChatResponse(BaseModel):
    assistant_text: str
    used_memory: List[MemoryRef]
    latency_ms: int
    token_usage: TokenUsage
