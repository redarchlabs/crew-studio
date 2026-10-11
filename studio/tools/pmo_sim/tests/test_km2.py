from __future__ import annotations

import socket

import pytest

from pmo_sim import km2
from pmo_sim.km2 import KM2Client, KM2Error, ReadOnlyError, multipart
from pmo_sim.tests.conftest import Sleeps, install_schema, make_client
from pmo_sim.tests.fake_km2 import FakeKM2

PERSON = {"name": "Ada", "email": "ada@example.test"}


def test_429_waits_for_retry_after_then_succeeds(schema: FakeKM2, sleeps: Sleeps) -> None:
    client = make_client(schema, sleep=sleeps)
    schema.store.failures.append(("POST", 429, {"Retry-After": "7"}))
    rec = client.create_record("trk_person", PERSON)
    assert rec["email"] == "ada@example.test"
    assert sleeps == [7.0]
    assert schema.store.count("trk_person") == 1


def test_429_without_retry_after_uses_backoff(schema: FakeKM2, sleeps: Sleeps) -> None:
    client = make_client(schema, sleep=sleeps, backoff=0.5)
    schema.store.failures.extend([("GET", 429, {}), ("GET", 429, {"Retry-After": "soon"})])
    assert client.list_records("trk_person") == []
    assert sleeps == [0.5, 1.0]


def test_gives_up_after_max_retries(schema: FakeKM2, sleeps: Sleeps) -> None:
    client = make_client(schema, sleep=sleeps, max_retries=2)
    schema.store.failures.extend([("GET", 503, {})] * 3)
    with pytest.raises(KM2Error) as err:
        client.list_records("trk_person")
    assert err.value.status == 503
    assert len(sleeps) == 2


def test_post_is_never_retried_on_5xx_but_get_is(schema: FakeKM2, sleeps: Sleeps) -> None:
    client = make_client(schema, sleep=sleeps)
    client.create_record("trk_person", PERSON)
    with pytest.raises(KM2Error) as err:  # unique violation: KM2 2.6.1 answers a bare 500
        client.create_record("trk_person", PERSON)
    assert err.value.status == 500 and sleeps == []
    schema.store.failures.append(("POST", 503, {}))
    with pytest.raises(KM2Error):  # the write may have happened; the caller's resume logic handles it
        client.create_record("trk_person", {"name": "Bo", "email": "bo@example.test"})
    assert sleeps == []
    schema.store.failures.append(("PATCH", 502, {}))
    rec = client.list_records("trk_person")[0]
    client.update_record("trk_person", rec["id"], {"role": "QA"})
    assert len(sleeps) == 1


def test_retry_after_is_capped() -> None:
    assert km2._retry_after("100000", 1.0) == km2.MAX_RETRY_AFTER
    assert km2._retry_after("-3", 1.0) == 0.0


def test_repeated_cursor_and_bad_shapes_are_errors(schema: FakeKM2, monkeypatch: pytest.MonkeyPatch) -> None:
    client = make_client(schema)
    pages = iter([{"items": [{"id": 1}], "next_cursor": "c1"}, {"items": [], "next_cursor": "c1"}])
    monkeypatch.setattr(client, "request", lambda *a, **k: next(pages))
    with pytest.raises(KM2Error, match="same page cursor"):
        client.list_records("trk_person")
    monkeypatch.setattr(client, "request", lambda *a, **k: ["not", "a", "page"])
    with pytest.raises(KM2Error, match="unexpected response shape"):
        client.list_records("trk_person")


def test_dropped_connections_are_retried_for_gets_only(
    schema: FakeKM2, sleeps: Sleeps, monkeypatch: pytest.MonkeyPatch
) -> None:
    client = make_client(schema, sleep=sleeps, max_retries=1)
    real_open = client._opener.open
    calls = {"n": 0}

    def flaky(req, timeout):  # type: ignore[no-untyped-def]
        calls["n"] += 1
        if calls["n"] == 1:
            raise ConnectionResetError("reset by peer")
        return real_open(req, timeout=timeout)

    monkeypatch.setattr(client._opener, "open", flaky)
    assert client.list_records("trk_person") == []
    assert sleeps == [1.0]
    calls["n"] = 0
    with pytest.raises(KM2Error, match="connection to"):
        client.create_record("trk_person", PERSON)


def test_non_json_success_is_an_error(schema: FakeKM2, monkeypatch: pytest.MonkeyPatch) -> None:
    client = make_client(schema)

    class Resp:
        status = 200

        def __enter__(self):  # type: ignore[no-untyped-def]
            return self

        def __exit__(self, *a: object) -> None:
            return None

        def read(self, _n: int) -> bytes:
            return b"<html>proxy page</html>"

    monkeypatch.setattr(client._opener, "open", lambda req, timeout: Resp())
    with pytest.raises(KM2Error, match="non-JSON"):
        client.list_records("trk_person")


def test_errors_carry_km2_detail(schema: FakeKM2) -> None:
    client = make_client(schema)
    with pytest.raises(KM2Error) as err:
        client.create_record("trk_person", {"name": "x", "email": "y", "nope": 1})
    assert err.value.status == 400
    assert "unknown fields" in err.value.detail
    assert "POST /api/v1/entities/trk_person/records -> 400" in str(err.value)
    with pytest.raises(KM2Error) as err:
        make_client(schema, "km2_wrong").list_records("trk_person")
    assert err.value.status == 401


def test_detail_parsing() -> None:
    assert km2._detail(b'{"detail": "boom"}') == "boom"
    assert km2._detail(b'{"detail": {"message": "x"}}') == '{"message": "x"}'
    assert km2._detail(b"plain text") == "plain text"
    assert km2._detail(b"") == "(empty response)"
    assert km2._detail(b"[1, 2]") == "[1, 2]"


def test_read_only_client_refuses_writes(schema: FakeKM2) -> None:
    client = make_client(schema, read_only=True)
    with pytest.raises(ReadOnlyError):
        client.create_record("trk_person", PERSON)
    assert client.list_records("trk_person") == []
    assert schema.store.writes == 0


def test_cursor_pagination_and_filters(schema: FakeKM2) -> None:
    client = make_client(schema)
    for i in range(7):
        client.create_record("trk_cycle", {"name": f"C{i}", "number": i, "project_key": "ERP" if i % 2 else "X"})
    pages = list(client.iter_records("trk_cycle", order_by="number", page_size=2))
    assert [r["number"] for r in pages] == list(range(7))
    erp = client.list_records("trk_cycle", ["project_key:eq:ERP", "number:gte:3"], order_by="number", order_dir="desc")
    assert [r["number"] for r in erp] == [5, 3]
    assert client.requests >= 5


def test_list_in_chunks_values(schema: FakeKM2, monkeypatch: pytest.MonkeyPatch) -> None:
    client = make_client(schema)
    for i in range(5):
        client.create_record("trk_cycle", {"name": f"C{i}", "number": i})
    monkeypatch.setattr(km2, "MAX_IN_VALUES", 2)
    found = client.list_in("trk_cycle", "number", ["0", "1", "2", "4"])
    assert sorted(r["number"] for r in found) == [0, 1, 2, 4]


def test_entities_and_delete(schema: FakeKM2) -> None:
    client = make_client(schema)
    assert client.get_entity("trk_issue")["slug"] == "trk_issue"
    assert client.get_entity("trk_nothing") is None
    rec = client.create_record("trk_person", PERSON)
    client.update_record("trk_person", rec["id"], {"role": "QA"})
    client.delete_record("trk_person", rec["id"])
    client.delete_record("trk_person", rec["id"])  # 404 is fine: already gone
    with pytest.raises(KM2Error):
        client.update_record("trk_person", rec["id"], {"role": "QA"})


def test_get_entity_reraises_other_errors(fake: FakeKM2) -> None:
    with pytest.raises(KM2Error) as err:
        make_client(fake, "km2_unknown").get_entity("trk_issue")
    assert err.value.status == 401


def test_unreachable_host_is_a_clear_error(sleeps: Sleeps) -> None:
    sock = socket.socket()
    sock.bind(("127.0.0.1", 0))
    port = sock.getsockname()[1]
    sock.close()
    client = KM2Client(f"http://127.0.0.1:{port}", "km2_x", min_interval=0, sleep=sleeps, max_retries=1)
    with pytest.raises(KM2Error) as err:
        client.list_records("trk_person")
    assert err.value.status == 0 and "cannot reach" in err.value.detail
    assert len(sleeps) == 1
    with pytest.raises(KM2Error):
        client.create_record("trk_person", PERSON)  # POST: no retry on connection errors
    assert len(sleeps) == 1


def test_throttle_spaces_requests(schema: FakeKM2, sleeps: Sleeps) -> None:
    now = [100.0]
    client = make_client(schema, sleep=sleeps, min_interval=0.5, clock=lambda: now[0])
    client.list_records("trk_person")
    client.list_records("trk_person")
    assert sleeps == [0.5]


def test_promote_dry_run_and_apply(fake: FakeKM2) -> None:
    client = make_client(fake, fake.BOOTSTRAP_KEY)
    preview = client.promote(b'{"kind": "km2-migration-bundle", "format_version": 2, "resources": {}}', dry_run=True)
    assert preview["dry_run"] is True
    with pytest.raises(KM2Error) as err:
        client.promote(b'{"kind": "other"}', dry_run=True)
    assert err.value.status == 400
    with pytest.raises(KM2Error) as err:  # the simulator key has no config:write scope
        make_client(fake).promote(b"{}", dry_run=True)
    assert err.value.status == 403
    install_schema(fake)
    assert len(fake.store.entities) == 8


def test_multipart_shape() -> None:
    body, ctype = multipart("file", "b.json", b"{}")
    boundary = ctype.split("boundary=")[1]
    assert body.startswith(f"--{boundary}\r\n".encode())
    assert b'name="file"; filename="b.json"' in body
    assert body.endswith(f"--{boundary}--\r\n".encode())
