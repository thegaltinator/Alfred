#!/usr/bin/env python3
"""
Planner tool shim that calls OpenAI via the Python SDK and returns structured JSON.
Reads a single JSON object from stdin and writes the plan JSON to stdout.
"""

import json
import os
import sys
from typing import List, Optional

from openai import OpenAI
from pydantic import BaseModel, Field


class PlanBlock(BaseModel):
    title: str = Field(..., description="Short label for the block")
    start_time: str = Field(..., description="ISO-8601 timestamp with timezone")
    end_time: str = Field(..., description="ISO-8601 timestamp with timezone")
    description: Optional[str] = None
    location: Optional[str] = None
    priority: Optional[str] = None
    tags: List[str] = Field(default_factory=list)
    all_day: bool = False


class PlannerResponse(BaseModel):
    notes: List[str] = Field(default_factory=list)
    plan_blocks: List[PlanBlock]


PROMPT_TEMPLATE = """You are Alfred's calendar strategist optimizing flow state, energy cycles, travel buffers, and contingency plans.
Date: {plan_date}.
Requests: "{time_block}".
Context: {activity_type}

Return JSON matching:
{{
  "notes": ["short reminders"],
  "plan_blocks": [
    {{
      "title": "Deep work sprint",
      "start_time": "2025-11-15T09:00:00-08:00",
      "end_time": "2025-11-15T11:00:00-08:00",
      "description": "prep + focus guidance",
      "location": "optional",
      "priority": "high | medium | low",
      "tags": ["focus","travel"],
      "all_day": false
    }}
  ]
}}

Rules:
- include timezone offsets
- keep plan_blocks <= 12 entries
- keep descriptions <= 140 characters
- never emit prose outside JSON
"""


def build_prompt(plan_date: str, time_block: str, activity_type: str) -> str:
    ctx = activity_type or "general productivity"
    return PROMPT_TEMPLATE.format(
        plan_date=plan_date,
        time_block=time_block,
        activity_type=ctx,
    )


def call_openai(plan_date: str, time_block: str, activity_type: str) -> PlannerResponse:
    client = OpenAI()
    prompt = build_prompt(plan_date, time_block, activity_type)
    response = client.responses.parse(
        model="gpt-5-mini",
        reasoning={"effort": "medium"},
        input=[
            {
                "role": "system",
                "content": (
                    "You are Alfred's calendar strategist, maximizing flow state, energy, and contingency coverage."
                    " Output structured JSON only."
                ),
            },
            {"role": "user", "content": prompt},
        ],
        text_format=PlannerResponse,
    )
    plan = response.output_parsed
    if plan is None:
        raise ValueError(f"model failed to parse structured response: {response.output}")
    if not plan.plan_blocks:
        raise ValueError("model returned no plan_blocks")
    return plan


def main() -> None:
    try:
        payload = json.load(sys.stdin)
    except json.JSONDecodeError as exc:
        raise SystemExit(f"invalid json input: {exc}") from exc

    plan_date = payload.get("plan_date") or ""
    time_block = payload.get("time_block") or ""
    activity_type = payload.get("activity_type") or ""

    if not time_block:
        raise SystemExit("time_block is required")

    try:
        plan = call_openai(plan_date, time_block, activity_type)
    except Exception as exc:  # pylint: disable=broad-except
        print(json.dumps({"error": str(exc)}), file=sys.stderr)
        raise SystemExit(1) from exc

    sys.stdout.write(plan.model_dump_json())


if __name__ == "__main__":
    if not os.environ.get("OPENAI_API_KEY"):
        raise SystemExit("OPENAI_API_KEY environment variable is required")
    main()
