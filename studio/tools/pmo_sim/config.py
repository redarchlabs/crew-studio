"""Settings from the environment and ``studio/tools/pmo_sim/.env`` (environment variables win)."""

from __future__ import annotations

import ipaddress
import os
from dataclasses import dataclass
from pathlib import Path
from urllib.parse import urlparse

DEFAULT_ENV_FILE = Path(__file__).resolve().parent / ".env"
KEY_PREFIX = "km2_"


class ConfigError(Exception):
    """A missing or malformed setting."""


@dataclass(frozen=True)
class Settings:
    base_url: str
    api_key: str
    bootstrap_key: str


def parse_env_file(path: Path) -> dict[str, str]:
    """Minimal ``KEY=value`` parser: comments, blank lines, ``export`` and surrounding quotes."""
    values: dict[str, str] = {}
    if not path.is_file():
        return values
    for raw in path.read_text(encoding="utf-8").splitlines():
        line = raw.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        key, _, value = line.removeprefix("export ").partition("=")
        value = value.strip()
        if len(value) >= 2 and value[0] == value[-1] and value[0] in "'\"":
            value = value[1:-1]
        values[key.strip()] = value
    return values


def _is_loopback(host: str) -> bool:
    if host == "localhost":
        return True
    try:
        return ipaddress.ip_address(host).is_loopback
    except ValueError:
        return False


def validate_base_url(url: str) -> str:
    parsed = urlparse(url)
    if parsed.scheme not in ("http", "https") or not parsed.hostname:
        raise ConfigError(f"KM2_BASE_URL must be an http(s) URL, got {url!r}")
    if parsed.scheme == "http" and not _is_loopback(parsed.hostname):
        raise ConfigError("KM2_BASE_URL must use https (plain http is only allowed for localhost)")
    if parsed.path not in ("", "/") or parsed.query or parsed.fragment:
        raise ConfigError("KM2_BASE_URL is the site root, e.g. https://app.example.com (no path)")
    return url.rstrip("/")


def load_settings(env_file: Path | None = None, environ: dict[str, str] | None = None) -> Settings:
    file_values = parse_env_file(env_file or DEFAULT_ENV_FILE)
    env = os.environ if environ is None else environ

    def get(name: str) -> str:
        return (env.get(name) or file_values.get(name) or "").strip()

    base_url = get("KM2_BASE_URL")
    if not base_url:
        raise ConfigError("KM2_BASE_URL is not set (environment or studio/tools/pmo_sim/.env)")
    return Settings(
        base_url=validate_base_url(base_url), api_key=get("KM2_API_KEY"), bootstrap_key=get("KM2_BOOTSTRAP_KEY")
    )


def require_key(value: str, name: str) -> str:
    if not value:
        raise ConfigError(f"{name} is not set (environment or studio/tools/pmo_sim/.env)")
    if not value.startswith(KEY_PREFIX):
        raise ConfigError(f"{name} does not look like a KM2 API key (expected it to start with {KEY_PREFIX!r})")
    return value
