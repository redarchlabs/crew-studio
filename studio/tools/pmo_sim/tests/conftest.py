from __future__ import annotations

import sys
from collections.abc import Iterator
from datetime import date
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))  # studio/tools, so `import pmo_sim` works

from pmo_sim import commands
from pmo_sim.km2 import KM2Client
from pmo_sim.tests.fake_km2 import FakeKM2

TODAY = date(2026, 10, 5)  # a Monday; project start = 2026-09-14
SEED = 20261005


def quiet(_: str) -> None:
    return None


class Sleeps(list):
    def __call__(self, seconds: float) -> None:
        self.append(seconds)


@pytest.fixture
def fake() -> Iterator[FakeKM2]:
    with FakeKM2() as server:
        yield server


@pytest.fixture
def sleeps() -> Sleeps:
    return Sleeps()


def make_client(server: FakeKM2, key: str | None = None, **kw: object) -> KM2Client:
    kw.setdefault("min_interval", 0)
    kw.setdefault("sleep", lambda _s: None)
    return KM2Client(server.url, key or server.API_KEY, **kw)  # type: ignore[arg-type]


def install_schema(server: FakeKM2) -> None:
    assert commands.cmd_bootstrap(make_client(server, server.BOOTSTRAP_KEY), True, quiet) == 0


@pytest.fixture
def schema(fake: FakeKM2) -> FakeKM2:
    install_schema(fake)
    return fake


@pytest.fixture
def client(fake: FakeKM2) -> KM2Client:
    return make_client(fake)


def seed_project(server: FakeKM2, seed: int = SEED) -> None:
    assert commands.cmd_seed(make_client(server), TODAY, seed, False, quiet) == 0


@pytest.fixture
def seeded(schema: FakeKM2) -> FakeKM2:
    seed_project(schema)
    return schema


@pytest.fixture
def cli_env(fake: FakeKM2, monkeypatch: pytest.MonkeyPatch) -> list[str]:
    monkeypatch.setenv("KM2_BASE_URL", fake.url)
    monkeypatch.setenv("KM2_API_KEY", fake.API_KEY)
    monkeypatch.setenv("KM2_BOOTSTRAP_KEY", fake.BOOTSTRAP_KEY)
    monkeypatch.setenv("PMO_SIM_TODAY", TODAY.isoformat())
    monkeypatch.setenv("PMO_SIM_MIN_INTERVAL", "0")
    return ["--env-file", str(Path(__file__).parent / "no-such.env")]
