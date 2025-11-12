from __future__ import annotations

import os
from typing import Any, Dict, Tuple

import httpx
import json

DEFAULT_BASE_URL = "https://api.cerebras.ai/v1"
DEFAULT_MODEL = "gpt-oss-120b"


def _env(*names: str) -> str | None:
    for name in names:
        val = os.environ.get(name)
        if val:
            return val
    return None


class CerberasClient:
    def __init__(self, base_url: str | None = None, api_key: str | None = None, model: str | None = None):
        self.base_url = base_url or _env("CERBERAS_BASE_URL", "CEREBRAS_BASE_URL") or DEFAULT_BASE_URL
        self.api_key = api_key or _env("CERBERAS_API_KEY", "CEREBRAS_API_KEY")
        self.model = model or _env("CERBERAS_MODEL", "CEREBRAS_MODEL") or DEFAULT_MODEL
        if not self.api_key:
            raise RuntimeError("CERBERAS_API_KEY missing")
        self._client = httpx.Client(timeout=httpx.Timeout(60.0))

    def complete(self, prompt: str, temperature: float = 0.4, stream: bool = False) -> Tuple[str, Dict[str, int]]:
        if stream:
            return self._complete_stream(prompt, temperature)
        return self._complete_non_stream(prompt, temperature)

    def _request_payload(self, prompt: str, temperature: float, stream: bool) -> Dict[str, Any]:
        return {
            "model": self.model,
            "messages": [
                {"role": "user", "content": prompt},
            ],
            "temperature": temperature,
            "stream": stream,
        }

    def _complete_non_stream(self, prompt: str, temperature: float) -> Tuple[str, Dict[str, int]]:
        url = f"{self.base_url.rstrip('/')}/chat/completions"
        headers = {
            "Authorization": f"Bearer {self.api_key}",
            "Content-Type": "application/json",
        }
        resp = self._client.post(url, headers=headers, json=self._request_payload(prompt, temperature, False))
        resp.raise_for_status()
        data = resp.json()
        text = self._extract_text(data)
        usage = data.get("usage", {})
        token_usage = {
            "prompt": int(usage.get("prompt_tokens", 0)),
            "completion": int(usage.get("completion_tokens", 0)),
        }
        return text, token_usage

    def _complete_stream(self, prompt: str, temperature: float) -> Tuple[str, Dict[str, int]]:
        url = f"{self.base_url.rstrip('/')}/chat/completions"
        headers = {
            "Authorization": f"Bearer {self.api_key}",
            "Content-Type": "application/json",
        }
        text_parts: list[str] = []
        usage_tokens: Dict[str, int] = {"prompt_tokens": 0, "completion_tokens": 0}
        with self._client.stream("POST", url, headers=headers, json=self._request_payload(prompt, temperature, True)) as resp:
            resp.raise_for_status()
            for raw_line in resp.iter_lines():
                if not raw_line:
                    continue
                line = raw_line.strip() if isinstance(raw_line, str) else raw_line.decode("utf-8", errors="ignore").strip()
                if not line:
                    continue
                if line.startswith("data:"):
                    line = line[5:].strip()
                if not line or line == "[DONE]":
                    if line == "[DONE]":
                        break
                    continue
                try:
                    chunk = json.loads(line)
                except json.JSONDecodeError:
                    continue
                choices = chunk.get("choices") or []
                if choices:
                    choice = choices[0]
                    delta = choice.get("delta") or {}
                    content = delta.get("content") or ""
                    if not content:
                        message = choice.get("message")
                        if message:
                            content = message.get("content", "")
                    if content:
                        text_parts.append(content)
                if "usage" in chunk:
                    usage_tokens = {
                        "prompt_tokens": int(chunk["usage"].get("prompt_tokens", usage_tokens.get("prompt_tokens", 0))),
                        "completion_tokens": int(chunk["usage"].get("completion_tokens", usage_tokens.get("completion_tokens", 0))),
                    }
        final_text = "".join(text_parts)
        return final_text, {
            "prompt": int(usage_tokens.get("prompt_tokens", 0)),
            "completion": int(usage_tokens.get("completion_tokens", 0)),
        }

    def _extract_text(self, data: Dict[str, Any]) -> str:
        choices = data.get("choices")
        if isinstance(choices, list) and choices:
            choice = choices[0]
            if "message" in choice and choice["message"].get("content"):
                return choice["message"]["content"]
            if choice.get("text"):
                return choice["text"]
        if "response" in data:
            return data["response"]
        if "output" in data and isinstance(data["output"], list):
            return data["output"][0].get("content", "")
        return ""
