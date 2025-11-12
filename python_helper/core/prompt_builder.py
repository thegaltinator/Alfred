from __future__ import annotations

import math
from dataclasses import dataclass
from typing import Iterable, List, Sequence

import tiktoken

ENCODING = tiktoken.get_encoding("cl100k_base")
MEMORY_TOKEN_BUDGET = 900
DEFAULT_SYSTEM_PROMPT = (
    "You are Alfred, an executive-function copilot. Use retrieved memory snippets when useful, "
    "otherwise reason from the user's latest input. Keep replies concise but specific."
)


@dataclass
class MemorySnippet:
    note_id: int
    text: str
    score: float
    ts: int


class PromptBuilder:
    def __init__(self, system_prompt: str | None = None, token_budget: int = MEMORY_TOKEN_BUDGET):
        self.system_prompt = system_prompt or DEFAULT_SYSTEM_PROMPT
        self.token_budget = token_budget

    def build(self, user_text: str, snippets: Sequence[MemorySnippet]) -> str:
        eligible = self._rank_and_cap(snippets)
        memory_block = self._render_memory_block(eligible)
        return (
            f"SYSTEM:\n{self.system_prompt}\n\n"
            f"CONTEXT_MEMORY:\n{memory_block}\n\n"
            f"USER:\n{user_text.strip()}"
        ).strip()

    def _rank_and_cap(self, snippets: Sequence[MemorySnippet]) -> List[MemorySnippet]:
        deduped: List[MemorySnippet] = []
        seen = set()
        for snippet in sorted(snippets, key=lambda s: (-s.score, -s.ts)):
            key = snippet.text.lower().strip()
            if not key or key in seen:
                continue
            seen.add(key)
            deduped.append(snippet)
        tokens_used = 0
        capped: List[MemorySnippet] = []
        for snippet in deduped:
            tokens = estimate_tokens(snippet.text)
            if tokens_used + tokens > self.token_budget:
                break
            capped.append(snippet)
            tokens_used += tokens
        return capped

    def _render_memory_block(self, snippets: Iterable[MemorySnippet]) -> str:
        lines = []
        for snip in snippets:
            lines.append(f"- (score={snip.score:.2f}) {snip.text}")
        return "\n".join(lines) if lines else "(none)"


def estimate_tokens(text: str) -> int:
    if not text:
        return 0
    return len(ENCODING.encode(text, disallowed_special=()))
