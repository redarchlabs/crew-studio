from __future__ import annotations

from pathlib import Path

import pytest

from pmo_sim.config import ConfigError, load_settings, parse_env_file, require_key, validate_base_url


def test_env_file_parsing(tmp_path: Path) -> None:
    env = tmp_path / ".env"
    env.write_text(
        "# comment\n\nexport KM2_BASE_URL='https://km2.example.test/'\nKM2_API_KEY=\"km2_abc\"\nNOEQUALS\n"
        "KM2_BOOTSTRAP_KEY=km2_boot\n"
    )
    assert parse_env_file(env) == {
        "KM2_BASE_URL": "https://km2.example.test/",
        "KM2_API_KEY": "km2_abc",
        "KM2_BOOTSTRAP_KEY": "km2_boot",
    }
    settings = load_settings(env, environ={"KM2_API_KEY": "km2_from_env"})
    assert settings.base_url == "https://km2.example.test"
    assert settings.api_key == "km2_from_env"
    assert settings.bootstrap_key == "km2_boot"


def test_missing_file_and_base_url(tmp_path: Path) -> None:
    assert parse_env_file(tmp_path / "nope") == {}
    with pytest.raises(ConfigError, match="KM2_BASE_URL is not set"):
        load_settings(tmp_path / "nope", environ={})


@pytest.mark.parametrize(
    ("url", "ok"),
    [
        ("https://app.example.test", True),
        ("http://127.0.0.1:8765", True),
        ("http://localhost:8000/", True),
        ("http://km2.example.test", False),
        ("ftp://km2.example.test", False),
        ("https://km2.example.test/api/v1", False),
        ("not a url", False),
    ],
)
def test_base_url_rules(url: str, ok: bool) -> None:
    if ok:
        assert not validate_base_url(url).endswith("/")
    else:
        with pytest.raises(ConfigError):
            validate_base_url(url)


def test_require_key() -> None:
    assert require_key("km2_x", "KM2_API_KEY") == "km2_x"
    with pytest.raises(ConfigError, match="not set"):
        require_key("", "KM2_API_KEY")
    with pytest.raises(ConfigError, match="start with"):
        require_key("sk-123", "KM2_API_KEY")
