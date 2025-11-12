import math
from core.prompt_builder import MemorySnippet, PromptBuilder, estimate_tokens


def test_prompt_builder_caps_memory_block():
    builder = PromptBuilder(token_budget=30)
    snippets = [
        MemorySnippet(note_id=i, text=("foo " * (i + 1)), score=1.0 - i * 0.1, ts=100 - i)
        for i in range(5)
    ]
    prompt = builder.build("Hello", snippets)
    # Ensure prompt contains limited number of snippets (budget 30 tokens ~ 120 chars)
    assert prompt.count("- (score=") <= 3
    assert "USER:\nHello" in prompt


def test_estimate_tokens_matches_length():
    short = "hello world"
    long = short * 20
    assert estimate_tokens(long) > estimate_tokens(short)
