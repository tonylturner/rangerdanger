"""Read-only simulator listener catalogue, transport proofs and G3 orchestration."""
from datetime import datetime
import hashlib
import http.client
import inspect
import ipaddress
import json
import math
from pathlib import Path
import socket
import subprocess
import time
import urllib.error
import urllib.request

from .containd_readiness import require_backend_protection

ROOT = Path(__file__).resolve().parents[2]
MATRIX = ROOT / "scripts/simulator-api-isolation-matrix.json"
POLICIES = ("weak", "improved")
WORKERS = 8
CONNECT_TIMEOUT = 2.5

# This same function is executed in the actual student container. No bind,
# payload, protocol operation, namespace helper or host-side network probe.
def socket_probe(target):
    row = dict(target)
    started = time.monotonic()
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
        sock.settimeout(2.5)
        row["started_at"] = time.time()
        try:
            sock.connect((row["host"], row["port"]))
            row["outcome"] = "connected"
        except TimeoutError:
            row["outcome"] = "timeout"
        except ConnectionRefusedError:
            row["outcome"] = "refused"
        except OSError as exc:
            row["outcome"] = "transport-error"
            row["error"] = str(exc)
            row["errno"] = exc.errno
        row["local"] = list(sock.getsockname())
    row["elapsed"] = time.monotonic() - started
    row["ended_at"] = time.time()
    return row


# Keep the remote program derived from the locally tested implementation.
SOCKET_PROGRAM = (
    "import concurrent.futures,json,socket,sys,time\n"
    + inspect.getsource(socket_probe)
    + "\nwith concurrent.futures.ThreadPoolExecutor(max_workers=8) as pool:\n"
    "    print(json.dumps(list(pool.map(socket_probe,json.loads(sys.argv[1])))))\n"
)
HTTP_PROGRAM = """
import json,sys,urllib.request
opener=urllib.request.build_opener(urllib.request.ProxyHandler({}))
with opener.open(sys.argv[1],timeout=3) as response:
    body=response.read(1048577)
    if len(body)>1048576:
        raise ValueError("oversized HTTP peer response")
    length=response.headers.get("Content-Length")
    if length is not None and len(body)!=int(length):
        raise ValueError("truncated HTTP peer response")
    print(json.dumps({"status":response.status,"body":body.decode()}))
"""
PREFLIGHT_PROGRAM = (
    "import concurrent.futures,json,socket,time; "
    "print(json.dumps({'ready':True,'hostname':socket.gethostname(),'time':time.time()}))"
)


def require(condition, message):
    if not condition:
        raise ValueError(message)


def load_inventory(package, root=ROOT, matrix_path=MATRIX):
    matrix = json.loads(Path(matrix_path).read_text())
    require(matrix["schema"] == 1 and package in matrix["packages"], "unknown package/schema")
    cat = matrix["packages"][package]
    base = Path(root) / "lab-definitions/packages" / package
    manifest = json.loads((base / "manifest.json").read_text())
    require(manifest["package"] == package and manifest["services"] == cat["services"],
            "manifest identities/aliases changed: review the G3 catalogue")
    for name, digest in cat["reviewed_inputs"].items():
        require(hashlib.sha256((base / name).read_bytes()).hexdigest() == digest,
                f"unclassified Compose change: {package}/{name}; review listeners and aliases")
    services = {s["key"]: s for s in cat["services"]}
    require(len(services) == len(cat["services"]), "duplicate service")
    require(cat["sources"] and cat["destinations"], "empty catalogue")
    require(set(cat["reviewed_inputs"]) == {"compose.source.yml", "compose.release.yml"},
            "both Compose modes must be reviewed")
    require(len(set(cat["sources"])) == len(cat["sources"]), "duplicate source")
    require(all(s in services for s in cat["sources"]), "unknown source")
    seen = set()
    for d in cat["destinations"]:
        ipaddress.IPv4Address(d["host"])
        require(type(d["port"]) is int and 0 < d["port"] < 65536, "invalid port")
        key = (d["host"], d["port"])
        require(key not in seen, "duplicate alias")
        seen.add(key)
        require(d["service"] in services and d["host"] in
                [i["ipv4"] for i in services[d["service"]]["interfaces"]], "unknown alias")
        require(set(d["expected"]) == set(POLICIES) and
                set(d["expected"].values()) <= {"A", "D"}, "invalid verdict/policy")
        for source, overrides in d.get("overrides", {}).items():
            require(source in cat["sources"] and set(overrides) <= set(POLICIES) and
                    set(overrides.values()) <= {"A", "D"}, "invalid source override")
        c = d["control"]
        require(c["service"] in services and c["tool"] in ("python3", "wget", "nc"),
                "unclassified positive control")
        require(c["tool"] != "wget" or c["path"], "wget control needs a read path")
    return cat


def expand_matrix(cat, package, policy):
    require(policy in POLICIES, "unrecognised policy")
    rows = []
    for source in cat["sources"]:
        for d in cat["destinations"]:
            rows.append({
                "id": f"{package}/{policy}/{source}/{d['host']}:{d['port']}",
                "source": source, "host": d["host"], "port": d["port"],
                "expected": d.get("overrides", {}).get(source, {}).get(
                    policy, d["expected"][policy]),
            })
    require(rows, "zero targets")
    return rows


def check_rows(expected, actual, source_address, positive=False, not_before=None):
    require(isinstance(actual, list) and len(actual) == len(expected), "incomplete probe batch")
    checked = []
    for wanted, row in zip(expected, actual):
        require(isinstance(row, dict) and all(row.get(k) == v for k, v in wanted.items()),
                "wrong/reordered probe identity")
        require(row.get("outcome") in ("connected", "timeout", "refused", "transport-error"),
                "missing transport classification")
        local = row.get("local")
        require(isinstance(local, list) and len(local) == 2 and
                type(local[1]) is int and 0 < local[1] < 65536, "missing getsockname")
        ipaddress.IPv4Address(local[0])
        if source_address is not None:
            require(local[0] == source_address, f"unexpected student source address: {row}")
        require(all(type(row.get(k)) in (float, int) and math.isfinite(row[k])
                    for k in ("started_at", "ended_at", "elapsed")), "missing range clock")
        require(row["ended_at"] >= row["started_at"] and row["elapsed"] >= 0,
                "invalid probe timing")
        require(abs(row["ended_at"] - row["started_at"] - row["elapsed"]) <= 2,
                "inconsistent probe clock")
        if not_before is not None:
            require(row["started_at"] >= not_before, "probe predates positive control window")
        if row["outcome"] == "timeout":
            require(row["elapsed"] >= CONNECT_TIMEOUT - .1, "premature connect timeout")
        observed = {"connected": "A", "timeout": "D"}.get(row["outcome"])
        passed = observed == ("A" if positive else wanted["expected"])
        checked.append(dict(row, observed=observed, passed=passed))
    return checked


def listener_ports(raw):
    """Decode Linux proc TCP tables, retaining bound addresses for alias review."""
    result = []
    for line in raw.splitlines():
        fields = line.split()
        if len(fields) < 4 or fields[3] != "0A":
            continue
        host, port = fields[1].split(":")
        if len(host) == 8:
            address = str(ipaddress.IPv4Address(bytes.fromhex(host)[::-1]))
        else:
            address = str(ipaddress.IPv6Address(b"".join(
                bytes.fromhex(host[i:i + 8])[::-1] for i in range(0, 32, 8))))
        result.append((address, int(port, 16)))
    return result


def check_listeners(cat, service, raw, record_loopback=None):
    """Catalogue the externally reachable surface; retain namespace-local evidence."""
    bound = listener_ports(raw)
    require(bound, f"no listeners: {service}")
    external = []
    # Record all loopbacks before validating any external listener, so an
    # unclassified bind earlier in the proc tables cannot hide this evidence.
    for host, port in bound:
        address = ipaddress.ip_address(host)
        if isinstance(address, ipaddress.IPv6Address) and address.ipv4_mapped is not None:
            address = address.ipv4_mapped
        if address.is_loopback:
            if record_loopback is not None:
                record_loopback(host, port)
        else:
            external.append((str(address), port))
    dest = [d for d in cat["destinations"] if d["service"] == service]
    exclusions = [d for d in cat["excluded_listeners"] if d["service"] == service]
    ports = {d["port"] for d in dest + exclusions}
    interfaces = {i["ipv4"] for s in cat["services"] if s["key"] == service for i in s["interfaces"]}
    allowed = {(d["host"], d["port"]) for d in dest}
    allowed.update((host, d["port"]) for host in interfaces for d in exclusions)
    observed = set()
    for host, port in external:
        require(port in ports, f"unclassified listener {service} {host}:{port}")
        aliases = interfaces if host in ("0.0.0.0", "::") else {host}
        observed.update((alias, port) for alias in aliases)
        require(all((alias, port) in allowed for alias in aliases),
                f"unclassified listener alias {service} {host}:{port}")
    missing = {(d["host"], d["port"]) for d in dest} - observed
    require(not missing, f"missing listener: {service}: {sorted(missing)}")


def timestamp(value):
    parsed = datetime.fromisoformat(value.replace("Z", "+00:00"))
    require(parsed.tzinfo is not None, "range timestamp has no timezone")
    return parsed.timestamp()


def fresh(value, now, age=15):
    require(isinstance(value, str) and -2 <= now - timestamp(value) <= age,
            f"stale/future timestamp: {value}, range time {now}")


def check_backend_controls(package, bodies, generation, now, previous=None, point_ids=()):
    """Application positives are separate from the labelled exact-address peers."""
    if package.startswith("eu-"):
        field, cc, historian = (bodies[p] for p in
                                ("/api/field/state", "/api/operator/state", "/api/historian/points"))
        devices = {"breaker-1", "f2-breaker", "rmu-1", "rmu-2", "oltc-1"}
        require(set(field["devices"]) == devices and all(field["devices"].values()) and
                isinstance(field["clock"], dict) and field["clock"], "missing EU devices/clock")
        process = field["process"]
        require(process.get("truth_stale") is False and type(process["solve_seq"]) is int and
                process["solve_seq"] > 0 and process.get("solver_epoch"), "unhealthy solver")
        fresh(process["solved_at"], now)
        require(cc["package_id"] == package and cc["range_generation"] == generation and
                cc["link"]["state"] == "started" and cc["points"], "unhealthy/wrong-generation CC")
        fresh(cc["rendered_at"], now)
        fresh(cc["link"]["last_rx_at"], now)
        require(isinstance(historian, list) and len(historian) == len(point_ids) and
                {p["id"] for p in historian} == set(point_ids), "incomplete historian inventory")
        for p in historian:
            fresh(p["last"]["ingested_at"], now)
        marker = (process["solver_epoch"], process["solve_seq"], timestamp(process["solved_at"]))
        if previous:
            require(marker[0] == previous[0] and marker[1] > previous[1] and marker[2] > previous[2],
                    "cached/restarted solver positive")
    else:
        state, health = (bodies[p] for p in ("/api/substation/state", "/api/substation/health"))
        devices = {"relay", "recloser", "regulator", "capbank"}
        require(set(state["devices"]) == devices and all(state["devices"].values()),
                "incomplete US devices")
        for obj in (state, health):
            require(set(obj["device_comms"]) == devices and
                    all(v is True for v in obj["device_comms"].values()), "US device comms failed")
            require(obj["physics"]["stale"] is False, "stale US physics")
            fresh(obj["physics"]["solved_at"], now)
            fresh(obj["physics"]["last_good_at"], now)
        require(health["status"] == "ok", "unhealthy US RTAC")
        marker = (timestamp(state["physics"]["last_good_at"]),
                  timestamp(state["physics"]["solved_at"]))
        if previous:
            require(all(a > b for a, b in zip(marker, previous)), "cached US physics positive")
    return marker


class Evidence:
    """A single chronological gate record, with unmodified transport inputs."""
    def __init__(self, directory, package):
        self.directory = Path(directory)
        self.directory.mkdir(parents=True, exist_ok=True)
        self.record = {"gate": "G3", "package": package, "events": [], "passed": False}
        self.flush()

    def flush(self):
        (self.directory / "G3.json").write_text(json.dumps(self.record, indent=2) + "\n")

    def event(self, kind, **values):
        self.record["events"].append(dict(sequence=len(self.record["events"]), kind=kind, **values))
        self.flush()

    def raw(self, kind, text):
        name = f"{len(self.record['events']):04d}-{kind}.txt"
        if isinstance(text, bytes):
            (self.directory / name).write_bytes(text)
        else:
            (self.directory / name).write_text(text)
        return name


class ReadOnlyRedirects(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        # urllib otherwise turns a redirected policy POST into a GET. Only
        # read-only portal redirects may be followed; never replay a mutation.
        if req.get_method() != "GET":
            return None
        return super().redirect_request(req, fp, code, msg, headers, newurl)


class Transport:
    def __init__(self, base_url, evidence, run=subprocess.run):
        require(base_url.startswith(("http://", "https://")), "invalid portal URL")
        self.base_url = base_url.rstrip("/")
        self.evidence = evidence
        self.run = run
        self.opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), ReadOnlyRedirects())

    def exec(self, container, argv, timeout=15):
        command = ["docker", "exec", container] + argv
        try:
            result = self.run(command, text=True, capture_output=True, timeout=timeout, check=False)
        except subprocess.TimeoutExpired as exc:
            raw = self.evidence.raw("exec-timeout", repr(
                {"error": str(exc), "stdout": exc.stdout, "stderr": exc.stderr}))
            self.evidence.event("exec", command=command, error="outer subprocess timeout", raw=raw)
            raise
        except OSError as exc:
            self.evidence.event("exec", command=command, error=str(exc))
            raise
        raw = self.evidence.raw("exec", json.dumps({"stdout": result.stdout, "stderr": result.stderr}))
        self.evidence.event("exec", command=command, returncode=result.returncode, raw=raw)
        require(result.returncode == 0, f"exec/tool/container failed ({result.returncode}): {command}")
        return result.stdout

    def http(self, path, body=None, json_body=True):
        data = None if body is None else json.dumps(body).encode()
        request = urllib.request.Request(self.base_url + path, data=data,
                                         method="GET" if body is None else "POST",
                                         headers={"Content-Type": "application/json"})
        try:
            with self.opener.open(request, timeout=15) as response:
                payload = response.read(1048577)
                raw = self.evidence.raw("http", payload)
                self.evidence.event("http", path=path, method=request.get_method(),
                                    status=response.status, raw=raw)
                require(response.status == 200 and len(payload) <= 1048576,
                        f"HTTP positive/apply failed: {path}")
                length = response.headers.get("Content-Length")
                require(length is None or len(payload) == int(length),
                        f"truncated HTTP response: {path}")
                text = payload.decode()
                return json.loads(text) if json_body else text
        except urllib.error.HTTPError as exc:
            try:
                raw = self.evidence.raw("http-error", exc.read(1048576))
            finally:
                exc.close()
            self.evidence.event("http-error", path=path, status=exc.code, raw=raw)
            raise  # In particular: never retry a 409.
        except http.client.IncompleteRead as exc:
            raw = self.evidence.raw("http-incomplete", exc.partial)
            self.evidence.event("http-error", path=path, error=str(exc), raw=raw)
            raise
        except (OSError, urllib.error.URLError) as exc:
            self.evidence.event("http-error", path=path, error=str(exc))
            raise


class Gate:
    def __init__(self, package, cat, transport, evidence):
        self.package, self.cat, self.io, self.evidence = package, cat, transport, evidence
        self.services = {s["key"]: s for s in cat["services"]}
        self.generation = None
        self.marker = None
        self.window_start = None
        self.student_rows = []

    def container(self, key):
        return self.services[key]["container"]

    def identity(self):
        identity = self.io.http("/api/range")
        require(isinstance(identity, dict) and identity.get("package") == self.package and
                identity.get("phase") == "ready" and
                type(identity.get("generation")) is int and identity["generation"] > 0,
                f"range identity/phase mismatch: {identity}")
        if self.generation is None:
            self.generation = identity["generation"]
        require(identity["generation"] == self.generation, "range generation changed")
        return identity

    def apply(self, policy):
        self.identity()
        receipt = require_backend_protection(self.io.http("/api/firewall/apply", {"config": policy}))
        self.evidence.event("policy", policy=policy, receipt=receipt)

    def preflight(self):
        for source in self.cat["sources"]:
            result = json.loads(self.io.exec(self.container(source),
                                             ["python3", "-c", PREFLIGHT_PROGRAM]))
            require(isinstance(result, dict) and result.get("ready") is True and
                    result.get("hostname"), f"student Python preflight failed: {source}")
            self.evidence.event("student-preflight", source=source, result=result)

    def probe(self, source, rows, positive=False):
        raw = self.io.exec(self.container(source), ["python3", "-c", SOCKET_PROGRAM,
                                                   json.dumps(rows)],
                           timeout=math.ceil(len(rows) / WORKERS) * 3 + 10)
        # Only student addresses are pinned, after the kernel selects them.
        address = None
        if not positive:
            address = next(i["ipv4"] for i in self.services[source]["interfaces"]
                           if i["network"] in ("enterprise_net", "vendor_net"))
        checked = check_rows(rows, json.loads(raw), address, positive,
                             None if positive else self.window_start)
        if not positive:
            self.student_rows.extend(checked)
        self.evidence.event("positive-tcp" if positive else "student-batch",
                            source=source, rows=checked, expected_count=len(rows),
                            observed_count=len(checked))
        require(all(r["passed"] for r in checked), f"transport verdict mismatch: {checked}")

    def peer_control(self, d):
        c = d["control"]
        container = self.container(c["service"])
        url = f"http://{d['host']}:{d['port']}{c['path'] or '/'}"
        if c["tool"] == "python3" and c["path"]:
            response = json.loads(self.io.exec(container, ["python3", "-c", HTTP_PROGRAM, url]))
            require(isinstance(response, dict) and response.get("status") == 200 and
                    isinstance(response.get("body"), str) and len(response["body"]) <= 1048576,
                    f"HTTP peer failed: {d}")
            require(isinstance(json.loads(response["body"]), (dict, list)),
                    "peer control is not a JSON application response")
        elif c["tool"] == "python3":
            self.probe(c["service"], [{"id": f"positive/{d['host']}:{d['port']}",
                                      "host": d["host"], "port": d["port"], "expected": "A"}],
                       positive=True)
        elif c["tool"] == "wget":
            body = self.io.exec(container, ["wget", "-qO-", "-T", "3", url])
            require(isinstance(json.loads(body), (dict, list)), "invalid peer HTTP body")
        else:
            # BusyBox nc connects without sending application bytes. Nonzero
            # (including timeout/refusal) never establishes listener health.
            self.io.exec(container, ["sh", "-c", 'exec nc -w 3 "$1" "$2" </dev/null',
                                     "sh", d["host"], str(d["port"])], timeout=6)
        self.evidence.event("peer-control", host=d["host"], port=d["port"], control=c)

    def controls(self, policy, stage):
        self.identity()
        now = self.range_clock()
        self.evidence.event("controls-start", policy=policy, stage=stage, range_time=now)
        if stage == "after":
            require(all(r["ended_at"] <= now for r in self.student_rows),
                    "student evidence extends beyond control window")
        for service in sorted({d["service"] for d in self.cat["destinations"]}):
            raw = self.io.exec(self.container(service), ["cat", "/proc/net/tcp", "/proc/net/tcp6"])
            check_listeners(self.cat, service, raw, record_loopback=lambda host, port:
                            self.evidence.event("loopback-listener", service=service,
                                                container=self.container(service),
                                                address=host, port=port, policy=policy, stage=stage))
        for d in self.cat["destinations"]:
            self.peer_control(d)
        # Portal routes specifically exercise the proxy's management aliases,
        # not a hypothetical direct backend->PLC/HMI application path.
        for path in (["/apps/openplc/"] if self.package.startswith("eu-")
                     else ["/apps/openplc/", "/apps/fuxa-hmi/"]):
            body = self.io.http(path, json_body=False).lower()
            require(("openplc" if "openplc" in path else "fuxa") in body,
                    f"portal response is not the upstream application: {path}")
        paths = (("/api/field/state", "/api/operator/state", "/api/historian/points")
                 if self.package.startswith("eu-")
                 else ("/api/substation/state", "/api/substation/health"))
        # Allow the normal solve cadence to advance, never retry a failed HTTP
        # call or unhealthy body. Only a valid, unchanged marker is polled.
        marker = None
        for attempt in range(6):
            start = self.range_clock()
            bodies = {p: self.io.http(p) for p in paths}
            end = self.range_clock()
            require(0 <= end - start <= 15, "application control window too long")
            marker = check_backend_controls(self.package, bodies, self.generation, end,
                                             point_ids=self.cat.get("point_ids", ()))
            if marker != self.marker:
                check_backend_controls(self.package, bodies, self.generation, end,
                                       self.marker, self.cat.get("point_ids", ()))
                break
            require(attempt < 5, "cached application positives did not advance")
            time.sleep(.5)
        self.marker = marker
        self.identity()
        self.evidence.event("controls-complete", policy=policy, stage=stage,
                            range_start=now, range_end=end, marker=self.marker)
        if stage == "before":
            self.window_start = end
            self.student_rows = []

    def range_clock(self):
        now = float(self.io.exec(self.container("kali"),
                                 ["python3", "-c", "import time; print(time.time())"]))
        require(math.isfinite(now) and now > 0, "invalid range clock")
        return now

    def run(self):
        self.evidence.event("catalogue", sources=self.cat["sources"],
                            destination_count=len(self.cat["destinations"]),
                            expected_counts={p: len(expand_matrix(self.cat, self.package, p))
                                             for p in POLICIES})
        self.identity()  # Never mutate or restore a foreign/unready range.
        failure = None
        try:
            self.preflight()
            for policy in POLICIES:
                self.apply(policy)
                rows = expand_matrix(self.cat, self.package, policy)
                self.evidence.event("policy-start", policy=policy, expected_count=len(rows))
                self.controls(policy, "before")
                batch_error = None
                try:
                    for source in self.cat["sources"]:
                        self.probe(source, [r for r in rows if r["source"] == source])
                except Exception as exc:
                    batch_error = exc
                    self.evidence.event("student-batch-failed", policy=policy, error=str(exc),
                                        expected_count=len(rows), observed_count=len(self.student_rows))
                finally:
                    self.controls(policy, "after")
                if batch_error:
                    raise batch_error
                self.evidence.event("policy-complete", policy=policy, expected_count=len(rows),
                                    observed_count=len(self.student_rows), tuple_count=len(rows))
        except BaseException as exc:
            failure = exc
            self.evidence.event("failure", error=str(exc))
        finally:
            try:
                self.apply("weak")
                self.evidence.event("restored", policy="weak")
            except BaseException as exc:
                self.evidence.event("restoration-failed", error=str(exc))
                raise RuntimeError(f"Weak restoration failed: {exc}; original failure: {failure}") from exc
        if failure:
            raise failure
        self.evidence.record["passed"] = True
        self.evidence.flush()
