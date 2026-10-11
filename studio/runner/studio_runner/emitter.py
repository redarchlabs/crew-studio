"""Writes run events to the Go service as JSON lines on a dedicated file descriptor.

Every event is also written in full to the run's transcript file (when one is given), so nothing is lost for
debugging; the live stream caps very long strings to keep the database and the browser responsive.
"""

import json
import threading
from datetime import datetime, timezone
from typing import Any, TextIO

# Long tool outputs are cut in the live stream; the transcript and the task output keep the full text.
MAX_TOOL_OUTPUT_CHARS = 4_000
# One runaway call cannot fill the database; the transcript keeps everything.
MAX_LLM_TEXT_CHARS = 200_000

STREAM_LIMITS = {"tool_finished": MAX_TOOL_OUTPUT_CHARS}
# The Go service reads final results from these, so they are never cut
UNCAPPED_TYPES = {"crew_completed", "crew_failed"}


def truncate(text: str, limit: int) -> str:
    if len(text) <= limit:
        return text
    return f"{text[:limit]}\n... [truncated {len(text) - limit:,} characters; full text in the run transcript]"


def cap_strings(value: Any, limit: int) -> Any:
    """Return a copy of value with every string longer than limit truncated."""
    if isinstance(value, str):
        return truncate(value, limit)
    if isinstance(value, dict):
        return {k: cap_strings(v, limit) for k, v in value.items()}
    if isinstance(value, list):
        return [cap_strings(v, limit) for v in value]
    return value


def to_jsonable(value: Any) -> Any:
    """Turn arbitrary CrewAI objects into something json.dumps accepts."""
    if value is None or isinstance(value, (str, int, float, bool)):
        return value
    if isinstance(value, dict):
        return {str(k): to_jsonable(v) for k, v in value.items()}
    if isinstance(value, (list, tuple, set)):
        return [to_jsonable(v) for v in value]
    if hasattr(value, "model_dump"):
        try:
            return to_jsonable(value.model_dump())
        except Exception:  # noqa: BLE001 - fall back to str for odd pydantic models
            return str(value)
    return str(value)


class Emitter:
    """Thread-safe JSON-lines writer. CrewAI runs event handlers on a thread pool."""

    def __init__(self, stream: TextIO, transcript: TextIO | None = None):
        self._stream = stream
        self._transcript = transcript
        self._lock = threading.Lock()

    def emit(self, event_type: str, payload: dict[str, Any], at: datetime | None = None) -> None:
        stamp = (at or datetime.now(timezone.utc)).isoformat()
        full = to_jsonable(payload)
        streamed = full if event_type in UNCAPPED_TYPES else cap_strings(full, STREAM_LIMITS.get(event_type, MAX_LLM_TEXT_CHARS))
        line = json.dumps({"type": event_type, "at": stamp, "payload": streamed}, ensure_ascii=False)
        with self._lock:
            if self._transcript is not None:
                try:
                    self._transcript.write(json.dumps({"type": event_type, "at": stamp, "payload": full}, ensure_ascii=False) + "\n")
                    self._transcript.flush()
                except (OSError, ValueError):
                    # A full disk must not stop the run; the live stream still has the event
                    self._transcript = None
            self._stream.write(line + "\n")
            self._stream.flush()

    def close(self) -> None:
        with self._lock:
            if self._transcript is not None:
                try:
                    self._transcript.close()
                except OSError:
                    pass
                self._transcript = None
