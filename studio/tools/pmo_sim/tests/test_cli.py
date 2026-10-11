from __future__ import annotations

import json
from pathlib import Path

import pytest

from pmo_sim import bundle, commands
from pmo_sim.__main__ import run
from pmo_sim.bundle import DELETE_ORDER
from pmo_sim.tests.conftest import make_client
from pmo_sim.tests.fake_km2 import FakeKM2


def cli(env: list[str], *args: str) -> tuple[int, list[str]]:
    lines: list[str] = []
    code = run([*env, *args], out=lines.append)
    return code, lines


def test_end_to_end_bootstrap_seed_advance_status(fake: FakeKM2, cli_env: list[str]) -> None:
    code, lines = cli(cli_env, "bootstrap-schema")
    assert code == 0 and fake.store.entities == {}
    assert lines[-1].startswith("Nothing applied")
    assert cli(cli_env, "bootstrap-schema", "--apply")[0] == 0
    assert len(fake.store.entities) == 8
    code, lines = cli(cli_env, "bootstrap-schema", "--apply")  # re-run: everything skipped, nothing breaks
    assert code == 0 and any("8 unchanged" in line for line in lines)

    code, lines = cli(cli_env, "seed", "--dry-run")
    assert code == 0 and fake.store.writes == 0
    assert cli(cli_env, "seed")[0] == 0
    code, lines = cli(cli_env, "advance", "--days", "10")
    assert code == 0 and lines[-1].startswith("Now at sim day 31")
    code, lines = cli(cli_env, "status")
    assert code == 0
    assert lines[0].startswith("ERP ↔ CRM Integration (ERP): sim day 31 = 2026-10-15 Thu")
    assert any(line.startswith("Overdue") for line in lines)
    assert any(line.startswith("Stale") for line in lines)
    assert any(line.startswith("Links:") for line in lines)


def test_bundle_command_writes_the_file(tmp_path: Path) -> None:
    out = tmp_path / "trk.json"
    code, lines = cli([], "bundle", "--out", str(out))
    assert code == 0 and "Migration -> Import" in lines[0]
    assert json.loads(out.read_text()) == bundle.build_bundle()


def test_reset_requires_yes_and_deletes_children_first(seeded: FakeKM2, cli_env: list[str]) -> None:
    cli(cli_env, "advance", "--days", "3")
    before = seeded.store.writes
    code, lines = cli(cli_env, "reset")
    assert code == 1 and seeded.store.writes == before
    assert lines[0].startswith("Would delete")
    seeded.store.request_log.clear()
    code, lines = cli(cli_env, "reset", "--yes")
    assert code == 0 and lines[-1].startswith("Done. Run `seed`")
    assert all(seeded.store.count(slug) == 0 for slug in DELETE_ORDER)
    assert len(seeded.store.entities) == 8  # the schema stays
    order = [path.split("/")[4] for method, path in seeded.store.request_log if method == "DELETE"]
    firsts = [order.index(slug) for slug in DELETE_ORDER if slug in order]
    assert firsts == sorted(firsts)
    assert order[-1] == "trk_person"
    assert cli(cli_env, "seed")[0] == 0  # can start over


def test_teardown_records_prints_manual_steps(seeded: FakeKM2, cli_env: list[str]) -> None:
    code, lines = cli(cli_env, "teardown-records", "--yes")
    assert code == 0
    assert seeded.store.count("trk_issue") == 0
    assert any("Admin -> Entities" in line for line in lines)
    assert any("delete the PMO Sandbox org" in line for line in lines)


def test_advance_days_must_be_positive(seeded: FakeKM2, cli_env: list[str]) -> None:
    code, lines = cli(cli_env, "advance", "--days", "0")
    assert code == 2 and lines == ["--days must be at least 1"]


def test_errors_exit_1_with_a_message(
    fake: FakeKM2, cli_env: list[str], monkeypatch: pytest.MonkeyPatch, capsys: pytest.CaptureFixture[str]
) -> None:
    assert cli(cli_env, "status")[0] == 1  # no schema: KM2 answers 404
    assert "error: KM2 GET" in capsys.readouterr().err
    monkeypatch.setenv("KM2_API_KEY", "nope")
    assert cli(cli_env, "status")[0] == 1
    assert "does not look like a KM2 API key" in capsys.readouterr().err
    monkeypatch.setenv("KM2_API_KEY", fake.API_KEY)
    monkeypatch.setenv("PMO_SIM_MIN_INTERVAL", "fast")
    assert cli(cli_env, "status")[0] == 1
    assert "PMO_SIM_MIN_INTERVAL" in capsys.readouterr().err


def test_status_without_seed(schema: FakeKM2, cli_env: list[str], capsys: pytest.CaptureFixture[str]) -> None:
    assert cli(cli_env, "status")[0] == 1
    assert "run `seed` first" in capsys.readouterr().err


def test_bootstrap_stops_on_dry_run_errors(fake: FakeKM2, monkeypatch: pytest.MonkeyPatch) -> None:
    broken = bundle.build_bundle()
    broken["resources"]["entities"][0]["fields"][0]["field_type"] = "picklist"  # no options -> KM2 rejects it
    monkeypatch.setattr(commands, "bundle_bytes", lambda: json.dumps(broken).encode())
    lines: list[str] = []
    assert commands.cmd_bootstrap(make_client(fake, fake.BOOTSTRAP_KEY), True, lines.append) == 1
    assert any(line.startswith("  error: entity 'trk_person'") for line in lines)
    assert lines[-1] == "The dry run reported problems; not applying."
    assert fake.store.entities == {}


def test_bootstrap_reports_apply_errors(fake: FakeKM2, monkeypatch: pytest.MonkeyPatch) -> None:
    client = make_client(fake, fake.BOOTSTRAP_KEY)
    calls: list[bool] = []

    def promote(payload: bytes, *, dry_run: bool) -> dict:
        calls.append(dry_run)
        summary = {"dry_run": dry_run, "resources": {"entities": {"created": 8, "failed": 0 if dry_run else 1}}}
        return {"diff": {"resources": []}, "import_summary": summary}

    monkeypatch.setattr(client, "promote", promote)
    assert commands.cmd_bootstrap(client, True, lambda _l: None) == 1
    assert calls == [True, False]
