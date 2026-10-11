"""``python -m pmo_sim <command>`` (run from studio/tools). See README.md."""

from __future__ import annotations

import argparse
import os
import sys
from collections.abc import Callable, Sequence
from datetime import date
from pathlib import Path

from . import blueprint as bp
from . import commands
from .config import ConfigError, load_settings, require_key
from .km2 import KM2Client, KM2Error, ReadOnlyError
from .seed import SeedError
from .state import StateError


def _today(value: str | None) -> date:
    raw = value or os.environ.get("PMO_SIM_TODAY")
    return date.fromisoformat(raw) if raw else date.today()


def _min_interval() -> float:
    """Seconds between requests (default 0.11, i.e. under KM2's 600/min per key); PMO_SIM_MIN_INTERVAL overrides."""
    raw = os.environ.get("PMO_SIM_MIN_INTERVAL")
    try:
        return max(0.0, float(raw)) if raw else 0.11
    except ValueError as exc:
        raise ConfigError(f"PMO_SIM_MIN_INTERVAL must be a number, got {raw!r}") from exc


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(prog="pmo_sim", description="ERP <-> CRM project simulator for KM2 trk_* entities")
    parser.add_argument("--env-file", type=Path, help="settings file (default: studio/tools/pmo_sim/.env)")
    parser.add_argument("--today", help="override the real date (YYYY-MM-DD); also PMO_SIM_TODAY")
    sub = parser.add_subparsers(dest="command", required=True)

    boot = sub.add_parser("bootstrap-schema", help="install the trk_* entities via config promotion (bootstrap key)")
    boot.add_argument("--apply", action="store_true", help="apply after a clean dry run (default: dry run only)")

    bundle = sub.add_parser("bundle", help="write the schema bundle for Admin -> Migration -> Import")
    bundle.add_argument("--out", type=Path, required=True)

    seed = sub.add_parser("seed", help="create the project as of day 21")
    seed.add_argument("--dry-run", action="store_true")
    seed.add_argument("--seed", type=int, default=bp.DEFAULT_SEED, help="random seed stored in trk_sim_state")

    adv = sub.add_parser("advance", help="simulate days (default: catch up to the real date)")
    adv.add_argument("--days", type=int, help="simulate exactly N days, even past the real date")
    adv.add_argument("--dry-run", action="store_true")

    sub.add_parser("status", help="summary of the project")

    reset = sub.add_parser("reset", help="delete every trk_* record, keep the schema (needs --yes)")
    reset.add_argument("--yes", action="store_true")
    teardown = sub.add_parser("teardown-records", help="reset, then print the manual teardown steps (needs --yes)")
    teardown.add_argument("--yes", action="store_true")
    return parser


def run(argv: Sequence[str] | None = None, out: Callable[[str], None] = print) -> int:
    args = build_parser().parse_args(argv)
    if args.command == "bundle":
        return commands.cmd_bundle(args.out, out)
    try:
        settings = load_settings(args.env_file)
        if args.command == "bootstrap-schema":
            client = KM2Client(
                settings.base_url,
                require_key(settings.bootstrap_key, "KM2_BOOTSTRAP_KEY"),
                min_interval=_min_interval(),
            )
            return commands.cmd_bootstrap(client, args.apply, out)
        dry = bool(getattr(args, "dry_run", False)) or (args.command in ("reset", "teardown-records") and not args.yes)
        client = KM2Client(
            settings.base_url, require_key(settings.api_key, "KM2_API_KEY"), read_only=dry, min_interval=_min_interval()
        )
        if args.command == "seed":
            return commands.cmd_seed(client, _today(args.today), args.seed, args.dry_run, out)
        if args.command == "advance":
            if args.days is not None and args.days < 1:
                out("--days must be at least 1")
                return 2
            return commands.cmd_advance(client, _today(args.today), args.days, args.dry_run, out)
        if args.command == "status":
            return commands.cmd_status(client, out)
        return commands.cmd_reset(client, args.yes, out, teardown=args.command == "teardown-records")
    except (ConfigError, KM2Error, StateError, SeedError, ReadOnlyError) as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 1


def main() -> None:  # pragma: no cover - thin wrapper
    sys.exit(run())


if __name__ == "__main__":  # pragma: no cover
    main()
