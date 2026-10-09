import asyncio
import json
import threading
from datetime import datetime, timezone
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import Self

from eu_app import create_eu_app
from eu_profile import load_profile
from eu_truth_loop import EUProcessState, TruthSolveLoop

SERVICE_DIR = Path(__file__).resolve().parents[1]
REPO_ROOT = SERVICE_DIR.parents[1]
PROCESS_DIR = REPO_ROOT / "lab-definitions/packages/eu-iec104-substation/process"


def truth(revision: int = 1, position: str = "closed") -> dict:
    return {
        "schema": 1,
        "device_id": "feeder-breaker",
        "generation": 98231,
        "revision": revision,
        "position": position,
        "remote_enabled": True,
        "lockout": False,
        "fault_active": False,
        "protection_tripped": False,
        "event_seq": revision,
        "changed_at": datetime.now(timezone.utc).isoformat().replace("+00:00", "Z"),
    }


class FakeDevice:
    def __init__(self) -> None:
        self.truths = [truth()]
        self.get_count = 0
        self.get_status = 200
        self.posts: list[dict] = []
        self.post_results: list[tuple[int, dict]] = []
        self.fail_get = False


class FakeTruthServer:
    def __init__(self, device: FakeDevice) -> None:
        self.device = device

        class Handler(BaseHTTPRequestHandler):
            def do_GET(self) -> None:
                if self.path != "/api/truth":
                    self.send_error(404)
                    return
                state = device
                if state.fail_get:
                    self.send_error(503)
                    return
                index = min(state.get_count, len(state.truths) - 1)
                payload = state.truths[index]
                state.get_count += 1
                self.respond(state.get_status, payload)

            def do_POST(self) -> None:
                if self.path != "/api/measurements":
                    self.send_error(404)
                    return
                size = int(self.headers.get("Content-Length", "0"))
                self.server.device.posts.append(json.loads(self.rfile.read(size)))
                if self.server.device.post_results:
                    status, payload = self.server.device.post_results.pop(0)
                else:
                    status, payload = (
                        200,
                        {
                            "accepted": True,
                            "revision": self.server.device.posts[-1]["revision"],
                        },
                    )
                self.respond(status, payload)

            def respond(self, status: int, payload: dict) -> None:
                encoded = json.dumps(payload).encode("utf-8")
                self.send_response(status)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(encoded)))
                self.end_headers()
                self.wfile.write(encoded)

            def log_message(self, _format: str, *_args: object) -> None:
                return

        class Server(ThreadingHTTPServer):
            daemon_threads = True

        self.server = self._bind(Server, Handler)
        self.server.device = device
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)

    @staticmethod
    def _bind(server_type, handler):
        for port in range(19000, 20000):
            try:
                return server_type(("127.0.0.1", port), handler)
            except OSError:
                continue
        raise RuntimeError("no available loopback port in the test range")

    @property
    def url(self) -> str:
        return f"http://127.0.0.1:{self.server.server_port}"

    def __enter__(self) -> Self:
        self.thread.start()
        return self

    def __exit__(self, *_args: object) -> None:
        self.server.shutdown()
        self.server.server_close()
        self.thread.join(timeout=2)


def make_profile(tmp_path: Path, truth_url: str):
    return load_profile(profile_file(tmp_path, truth_url))


def profile_file(tmp_path: Path, truth_url: str) -> Path:
    profile_data = json.loads(
        (PROCESS_DIR / "profile.json").read_text(encoding="utf-8")
    )
    profile_data["circuit_file"] = str(PROCESS_DIR / "feeder.dss")
    profile_data["device"]["truth_url"] = truth_url
    profile_path = tmp_path / "profile.json"
    profile_path.write_text(json.dumps(profile_data), encoding="utf-8")
    return profile_path


class SolverSpy:
    def __init__(self, results: list[dict] | None = None) -> None:
        self.calls: list[bool] = []
        self.results = results or []

    def solve(self, breaker_closed: bool) -> dict:
        self.calls.append(breaker_closed)
        if self.results:
            return self.results.pop(0)
        return {
            "source_voltage_kv_ll": 20.0,
            "load_voltage_kv_ll": 19.9 if breaker_closed else 0.0,
            "current_a": 28.9 if breaker_closed else 0.0,
            "load_kw": 1000.0 if breaker_closed else 0.0,
            "energized": breaker_closed,
            "converged": True,
            "solved_at": "2026-10-09T12:00:00.000Z",
        }


def test_stale_truth_conflict_rereads_and_resolves_once(tmp_path: Path) -> None:
    device = FakeDevice()
    device.truths = [truth(1), truth(2, "open")]
    device.post_results = [
        (409, {"accepted": False, "reason": "stale_truth"}),
        (200, {"accepted": True, "revision": 2}),
    ]
    with FakeTruthServer(device) as server:
        profile = make_profile(tmp_path, server.url)
        solver = SolverSpy()
        loop = TruthSolveLoop(profile, solver)
        asyncio.run(loop.run_tick())

    assert solver.calls == [True, False]
    assert [post["revision"] for post in device.posts] == [1, 2]
    assert [post["solve_seq"] for post in device.posts] == [1, 2]
    assert loop.state.electrical()["breaker"] == "open"
    assert loop.state.electrical()["current_a"] == 0.0


def test_truth_loss_marks_stale_and_does_not_post_again(tmp_path: Path) -> None:
    device = FakeDevice()
    now = [10.0]
    with FakeTruthServer(device) as server:
        profile = make_profile(tmp_path, server.url)
        state = EUProcessState(profile.truth_stale_after_ms, clock=lambda: now[0])
        solver = SolverSpy()
        loop = TruthSolveLoop(profile, solver, state=state, clock=lambda: now[0])
        asyncio.run(loop.run_tick())
        assert len(device.posts) == 1

        device.fail_get = True
        now[0] += 4.0
        asyncio.run(loop.run_tick())

    assert len(device.posts) == 1
    assert len(solver.calls) == 1
    assert state.electrical()["truth_stale"] is True
    assert state.health()["truth_stale"] is True
    assert state.health()["status"] == "degraded"


def test_loop_does_not_solve_or_post_before_first_truth(tmp_path: Path) -> None:
    device = FakeDevice()
    device.fail_get = True
    with FakeTruthServer(device) as server:
        profile = make_profile(tmp_path, server.url)
        solver = SolverSpy()
        loop = TruthSolveLoop(profile, solver)
        asyncio.run(loop.run_tick())
        electrical = loop.state.electrical()

    assert solver.calls == []
    assert device.posts == []
    assert electrical is None


def test_sequence_is_monotonic_and_each_start_gets_a_new_epoch(tmp_path: Path) -> None:
    device = FakeDevice()
    with FakeTruthServer(device) as server:
        profile = make_profile(tmp_path, server.url)
        first = TruthSolveLoop(profile, SolverSpy())
        asyncio.run(first.run_tick())
        asyncio.run(first.run_tick())
        second = TruthSolveLoop(profile, SolverSpy())
        asyncio.run(second.run_tick())

    assert [post["solve_seq"] for post in device.posts] == [1, 2, 1]
    assert device.posts[0]["solver_epoch"] == device.posts[1]["solver_epoch"]
    assert device.posts[2]["solver_epoch"] != device.posts[1]["solver_epoch"]


def test_nonconvergence_keeps_last_good_output_and_does_not_post(
    tmp_path: Path,
) -> None:
    device = FakeDevice()
    failure = {"converged": False, "solved_at": "2026-10-09T12:00:02.000Z"}
    with FakeTruthServer(device) as server:
        profile = make_profile(tmp_path, server.url)
        solver = SolverSpy(results=[SolverSpy().solve(True), failure])
        loop = TruthSolveLoop(profile, solver)
        asyncio.run(loop.run_tick())
        last_good = loop.state.electrical()

        asyncio.run(loop.run_tick())
        retained = loop.state.electrical()

    assert len(device.posts) == 1
    assert retained["current_a"] == last_good["current_a"]
    assert retained["solved_at"] == last_good["solved_at"]
    assert retained["solve_seq"] == last_good["solve_seq"]
    assert retained["converged"] is False
    assert loop.state.health()["last_converged"] is False


async def invoke(app, method: str, path: str) -> tuple[int, dict]:
    messages = []
    sent_request = False

    async def receive():
        nonlocal sent_request
        if not sent_request:
            sent_request = True
            return {"type": "http.request", "body": b"", "more_body": False}
        return {"type": "http.disconnect"}

    async def send(message):
        messages.append(message)

    await app(
        {
            "type": "http",
            "asgi": {"version": "3.0", "spec_version": "2.3"},
            "http_version": "1.1",
            "method": method,
            "scheme": "http",
            "path": path,
            "raw_path": path.encode(),
            "query_string": b"",
            "headers": [],
            "client": ("127.0.0.1", 19001),
            "server": ("127.0.0.1", 19002),
        },
        receive,
        send,
    )
    response = next(
        message for message in messages if message["type"] == "http.response.start"
    )
    body = next(
        message["body"]
        for message in messages
        if message["type"] == "http.response.body"
    )
    return response["status"], json.loads(body)


def test_eu_api_returns_no_truth_and_rejects_legacy_push(tmp_path: Path) -> None:
    app = create_eu_app(str(profile_file(tmp_path, "http://127.0.0.1:19999")))

    async def started_requests() -> tuple[tuple[int, dict], tuple[int, dict]]:
        async with app.router.lifespan_context(app):
            electrical = await invoke(app, "GET", "/api/electrical")
            update = await invoke(app, "POST", "/api/update-state")
            return electrical, update

    (electrical_status, electrical), (update_status, _) = asyncio.run(
        started_requests()
    )

    assert electrical_status == 503
    assert electrical == {"error": "no_truth"}
    assert update_status == 405
