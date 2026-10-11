"""CrewAI tool that searches Google through Gemini's Google Search grounding.

Gemini runs the Google searches, writes an answer from the results, and
returns the web sources it used, so the agent can cite where findings came from.
"""

import os

import httpx
from crewai.tools import BaseTool
from pydantic import BaseModel, Field

import budget_guard
from spend_tracker import record_search

GEMINI_API_URL = "https://generativelanguage.googleapis.com/v1beta/models/{model}:generateContent"
VERTEX_API_URL = "https://aiplatform.googleapis.com/v1/publishers/google/models/{model}:generateContent"
# Gemini API (AI Studio) keys start with this; Vertex AI keys (e.g. "AQ.") do not
GEMINI_API_KEY_PREFIX = "AIza"
DEFAULT_MODEL = "gemini-2.5-flash"
REQUEST_TIMEOUT_SECONDS = 60.0
MAX_OUTPUT_TOKENS = 4096


class GoogleSearchInput(BaseModel):
    query: str = Field(..., description="What to search the web for.")


class GoogleSearchTool(BaseTool):
    name: str = "Google Search"
    description: str = (
        "Searches Google for current information and returns a summarized "
        "answer with the source URLs it was based on."
    )
    args_schema: type[BaseModel] = GoogleSearchInput

    def _run(self, query: str) -> str:
        if not budget_guard.searches_allowed():
            return (
                "Search budget for this run is used up. Do not search again; "
                "write your final answer now from what you have already found."
            )

        api_key = os.getenv("GEMINI_API_KEY")
        if not api_key:
            return "Google Search unavailable: GEMINI_API_KEY is not set in .env."

        model = os.getenv("GEMINI_SEARCH_MODEL", DEFAULT_MODEL)
        url_template = GEMINI_API_URL if api_key.startswith(GEMINI_API_KEY_PREFIX) else VERTEX_API_URL
        payload = {
            "contents": [{"role": "user", "parts": [{"text": query}]}],
            "tools": [{"google_search": {}}],
            "generationConfig": {"temperature": 0.2, "maxOutputTokens": MAX_OUTPUT_TOKENS},
        }

        try:
            response = httpx.post(
                url_template.format(model=model),
                # Send the key as a header so it never appears in URLs or logs
                headers={"x-goog-api-key": api_key},
                json=payload,
                timeout=REQUEST_TIMEOUT_SECONDS,
            )
            response.raise_for_status()
        except httpx.HTTPStatusError as exc:
            return f"Google Search failed: HTTP {exc.response.status_code} - {_error_message(exc.response)}"
        except httpx.HTTPError as exc:
            return f"Google Search failed: {type(exc).__name__}: {exc}"

        data = response.json()
        record_search(model, data)
        return _format_result(data)


def _error_message(response: httpx.Response) -> str:
    try:
        return response.json()["error"]["message"]
    except (ValueError, KeyError, TypeError):
        return response.text[:500]


def _format_result(data: dict) -> str:
    candidate = (data.get("candidates") or [{}])[0]
    parts = candidate.get("content", {}).get("parts", [])
    answer = "".join(part.get("text", "") for part in parts).strip()
    if not answer:
        return "Google Search returned no answer for this query."

    metadata = candidate.get("groundingMetadata", {})
    sources = [
        chunk["web"] for chunk in metadata.get("groundingChunks", []) if "web" in chunk
    ]

    lines = [answer, ""]
    if sources:
        lines.append("Sources:")
        lines.extend(
            f"[{i}] {source.get('title', 'Untitled')} - {source.get('uri', '')}"
            for i, source in enumerate(sources, start=1)
        )
    else:
        lines.append("Sources: none returned (answer may not be grounded in a search).")

    queries = metadata.get("webSearchQueries", [])
    if queries:
        lines.append("")
        lines.append("Searches run: " + "; ".join(queries))

    return "\n".join(lines)
