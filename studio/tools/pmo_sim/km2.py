"""Small KM2 v1 API client (urllib only): records, entities and config promotion.

- Auth: ``Authorization: Bearer km2_...``.
- Records: ``/api/v1/entities/{slug}/records`` with ``filter=field:op:value`` (repeatable), ``order_by``,
  ``order_dir`` and keyset ``cursor`` pagination (at most 200 per page). Bodies are flat ``{field_slug: value}``
  dicts; to-one relationships are written and read as bare record ids under the relationship slug.
- Rate limits: 600 requests/min per key. A 429 is retried after its ``Retry-After``; 5xx answers are retried with
  exponential backoff for GET/PATCH/DELETE. A POST is never retried on 5xx or a dropped connection (the server may
  have done the write); the simulator's resume logic picks the work up on the next run instead.
"""

from __future__ import annotations

import http.client
import json
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid
from collections.abc import Callable, Iterator
from typing import Any

PAGE_SIZE = 200
MAX_IN_VALUES = 200
USER_AGENT = "crew-studio-pmo-sim/1.0"
MAX_RETRY_AFTER = 120.0
IDEMPOTENT = frozenset({"GET", "PATCH", "DELETE", "PUT"})
MAX_BODY_BYTES = 32 * 1024 * 1024


class KM2Error(Exception):
    """A KM2 request failed (after retries)."""

    def __init__(self, method: str, path: str, status: int, detail: str) -> None:
        self.method = method
        self.path = path
        self.status = status
        self.detail = detail
        super().__init__(f"KM2 {method} {path} -> {status}: {detail}")


class ReadOnlyError(Exception):
    """A write was attempted through a read-only (dry-run) client."""


class _NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *args: Any, **kwargs: Any) -> None:
        return None


def _detail(raw: bytes) -> str:
    text = raw.decode("utf-8", errors="replace").strip()
    try:
        body = json.loads(text)
    except json.JSONDecodeError:
        return text[:500] or "(empty response)"
    if isinstance(body, dict) and "detail" in body:
        detail = body["detail"]
        return detail if isinstance(detail, str) else json.dumps(detail)
    return text[:500]


def _retry_after(header: str | None, fallback: float) -> float:
    if not header:
        return fallback
    try:
        return min(MAX_RETRY_AFTER, max(0.0, float(header)))
    except ValueError:
        return fallback


def multipart(field: str, filename: str, payload: bytes, content_type: str = "application/json") -> tuple[bytes, str]:
    boundary = f"pmosim-{uuid.uuid4().hex}"
    head = (
        f"--{boundary}\r\n"
        f'Content-Disposition: form-data; name="{field}"; filename="{filename}"\r\n'
        f"Content-Type: {content_type}\r\n\r\n"
    ).encode()
    body = head + payload + f"\r\n--{boundary}--\r\n".encode()
    return body, f"multipart/form-data; boundary={boundary}"


class KM2Client:
    def __init__(
        self,
        base_url: str,
        api_key: str,
        *,
        timeout: float = 30.0,
        max_retries: int = 5,
        backoff: float = 1.0,
        min_interval: float = 0.11,
        read_only: bool = False,
        sleep: Callable[[float], None] = time.sleep,
        clock: Callable[[], float] = time.monotonic,
    ) -> None:
        self.base_url = base_url.rstrip("/")
        self._key = api_key
        self.timeout = timeout
        self.max_retries = max_retries
        self.backoff = backoff
        self.min_interval = min_interval
        self.read_only = read_only
        self._sleep = sleep
        self._clock = clock
        self._last = 0.0
        self._opener = urllib.request.build_opener(_NoRedirect)
        self.requests = 0
        self.writes = 0

    # ------------------------------------------------------------------ transport
    def _throttle(self) -> None:
        if self.min_interval <= 0:
            return
        wait = self._last + self.min_interval - self._clock()
        if wait > 0:
            self._sleep(wait)
        self._last = self._clock()

    def request(
        self,
        method: str,
        path: str,
        *,
        params: list[tuple[str, str]] | None = None,
        body: Any = None,
        raw: tuple[bytes, str] | None = None,
    ) -> Any:
        if method != "GET" and self.read_only:
            raise ReadOnlyError(f"dry run: refusing {method} {path}")
        url = self.base_url + path
        if params:
            url += "?" + urllib.parse.urlencode(params)
        headers = {"Authorization": f"Bearer {self._key}", "Accept": "application/json", "User-Agent": USER_AGENT}
        data: bytes | None = None
        if raw is not None:
            data, headers["Content-Type"] = raw
        elif body is not None:
            data = json.dumps(body).encode("utf-8")
            headers["Content-Type"] = "application/json"
        attempt = 0
        while True:
            self._throttle()
            self.requests += 1
            req = urllib.request.Request(url, data=data, method=method, headers=headers)
            try:
                with self._opener.open(req, timeout=self.timeout) as resp:
                    payload = resp.read(MAX_BODY_BYTES + 1)
                    if method != "GET":
                        self.writes += 1
                    if len(payload) > MAX_BODY_BYTES:
                        raise KM2Error(method, path, resp.status, "response too large")
                    return json.loads(payload) if payload.strip() else None
            except urllib.error.HTTPError as exc:
                status = exc.code
                raw_body = exc.read()
                retryable = status == 429 or (status >= 500 and method in IDEMPOTENT)
                if retryable and attempt < self.max_retries:
                    delay = self.backoff * (2**attempt)
                    if status == 429:
                        delay = _retry_after(exc.headers.get("Retry-After"), delay)
                    attempt += 1
                    self._sleep(delay)
                    continue
                raise KM2Error(method, path, status, _detail(raw_body)) from None
            except urllib.error.URLError as exc:
                if attempt < self.max_retries and method in IDEMPOTENT:
                    self._sleep(self.backoff * (2**attempt))
                    attempt += 1
                    continue
                raise KM2Error(method, path, 0, f"cannot reach {self.base_url}: {exc.reason}") from None
            except (OSError, http.client.HTTPException) as exc:  # timeouts, resets, truncated responses
                if attempt < self.max_retries and method in IDEMPOTENT:
                    self._sleep(self.backoff * (2**attempt))
                    attempt += 1
                    continue
                raise KM2Error(method, path, 0, f"connection to {self.base_url} failed: {exc!r}") from None
            except ValueError as exc:  # a 2xx that is not JSON
                raise KM2Error(method, path, 0, f"unexpected non-JSON response: {exc}") from None

    # ------------------------------------------------------------------ entities
    def get_entity(self, slug: str) -> dict[str, Any] | None:
        try:
            return self.request("GET", f"/api/v1/entities/{urllib.parse.quote(slug)}")
        except KM2Error as exc:
            if exc.status == 404:
                return None
            raise

    # ------------------------------------------------------------------ records
    def _records_path(self, slug: str, record_id: str | None = None) -> str:
        path = f"/api/v1/entities/{urllib.parse.quote(slug)}/records"
        return f"{path}/{urllib.parse.quote(record_id)}" if record_id else path

    def iter_records(
        self,
        slug: str,
        filters: list[str] | None = None,
        *,
        order_by: str | None = None,
        order_dir: str = "asc",
        page_size: int = PAGE_SIZE,
    ) -> Iterator[dict[str, Any]]:
        cursor: str | None = None
        while True:
            params: list[tuple[str, str]] = [("filter", f) for f in filters or []]
            params.append(("limit", str(min(page_size, PAGE_SIZE))))
            params.append(("order_dir", order_dir))
            if order_by:
                params.append(("order_by", order_by))
            if cursor:
                params.append(("cursor", cursor))
            page = self.request("GET", self._records_path(slug), params=params)
            if not isinstance(page, dict):
                raise KM2Error("GET", self._records_path(slug), 0, "unexpected response shape (expected an object)")
            yield from page.get("items") or []
            previous, cursor = cursor, page.get("next_cursor")
            if not cursor:
                return
            if cursor == previous:
                raise KM2Error("GET", self._records_path(slug), 0, "server repeated the same page cursor")

    def list_records(self, slug: str, filters: list[str] | None = None, **kw: Any) -> list[dict[str, Any]]:
        return list(self.iter_records(slug, filters, **kw))

    def list_in(self, slug: str, field: str, values: list[str], extra: list[str] | None = None) -> list[dict[str, Any]]:
        """Records whose ``field`` is one of ``values`` (chunked to KM2's 200-value ``in`` cap)."""
        out: list[dict[str, Any]] = []
        for i in range(0, len(values), MAX_IN_VALUES):
            chunk = values[i : i + MAX_IN_VALUES]
            out.extend(self.list_records(slug, [f"{field}:in:{','.join(chunk)}", *(extra or [])]))
        return out

    def create_record(self, slug: str, data: dict[str, Any]) -> dict[str, Any]:
        return self.request("POST", self._records_path(slug), body=data)

    def update_record(self, slug: str, record_id: str, patch: dict[str, Any]) -> dict[str, Any]:
        return self.request("PATCH", self._records_path(slug, record_id), body=patch)

    def delete_record(self, slug: str, record_id: str) -> None:
        try:
            self.request("DELETE", self._records_path(slug, record_id))
        except KM2Error as exc:
            if exc.status != 404:  # already gone (e.g. removed by a cascade)
                raise

    # ------------------------------------------------------------------ config promotion
    def promote(
        self, bundle: bytes, *, dry_run: bool, strategy: str = "skip", apply_deletes: bool = False
    ) -> dict[str, Any]:
        params = [
            ("dry_run", "true" if dry_run else "false"),
            ("strategy", strategy),
            ("apply_deletes", "true" if apply_deletes else "false"),
        ]
        body = multipart("file", "trk-schema-bundle.json", bundle)
        return self.request("POST", "/api/v1/config/promotions", params=params, raw=body)
