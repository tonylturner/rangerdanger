#!/usr/bin/env python3
"""US RTAC IPv4 host controls and one OT Ops -> physics transit path.

No route, rule, sysctl or network changes. AF_PACKET diagnostic datagrams do
not invoke an application. This proves one selected path, not all leg pairs
or IPv6. The L4 source pin enforces; DPI detects and logs.

The same source is streamed to existing containers, without installing files.
Keep helper paths compatible with OpenPLC's Python 3.6.
"""
import argparse
import datetime
import glob
import json
import os
from pathlib import Path
import shlex
import socket
import struct
import subprocess
import sys
import time
import urllib.error
import urllib.request
import uuid


RTAC = "rangerdanger-rtac-sim"
PLC = "rangerdanger-openplc"
DSS = "rangerdanger-opendss-sim"
OT = "10.30.30.20"
FIELD = "10.40.40.10"
PHYSICS = "10.50.50.10"
MGMT = "10.99.99.20"
SOURCE = "10.30.30.30"
DEST = "10.50.50.20"
GATEWAY = "10.30.30.2"
LEGS = (OT, FIELD, PHYSICS, MGMT)


def require(condition, message):
    if not condition:
        raise ValueError(message)


def stamp():
    return {"utc": datetime.datetime.now(datetime.timezone.utc).isoformat(),
            "monotonic": time.monotonic()}


def assert_interfaces(rows, addresses=LEGS):
    result = {}
    for address in addresses:
        matches = [row for row in rows if any(
            a.get("family") == "inet" and a.get("local") == address
            for a in row.get("addr_info", []))]
        require(len(matches) == 1, "interface_missing_or_duplicate: " + address)
        row = matches[0]
        require(row.get("ifname"), "interface_name_missing: " + address)
        require("UP" in row.get("flags", []), "interface_not_up: " + address)
        require(row.get("address"), "interface_mac_missing: " + address)
        result[address] = {"ifname": row["ifname"], "mac": row["address"]}
    require(len({r["ifname"] for r in result.values()}) == len(addresses),
            "interfaces_not_distinct")
    return result


def assert_forwarding(values, rows):
    required = {"/proc/sys/net/ipv4/ip_forward"}
    for name in {"all", "default", "lo"} | {r["ifname"] for r in rows}:
        required.add("/proc/sys/net/ipv4/conf/" + name + "/forwarding")
    require(required <= set(values), "forwarding_missing_or_unreadable")
    require(all(value == "0" for value in values.values()), "forwarding_not_zero")


def assert_forward_chain(text):
    require(text.splitlines() == ["-P FORWARD DROP"],
            "forward_chain_not_empty_drop: " + text)


def assert_field_input(text, field_if):
    lines = [shlex.split(line) for line in text.splitlines()]
    require(lines and lines[0] in (["-P", "INPUT", "ACCEPT"],
                                  ["-P", "INPUT", "DROP"]), "input_policy_invalid")
    target = ["-A", "INPUT", "-i", field_if, "-j", "DROP"]
    for rule in lines[1:]:
        if rule == target:
            return
        # Only an explicit, non-negated *different* interface proves disjoint.
        # Unknown chains, broad accepts and state matches fail conservatively.
        require(rule[:2] == ["-A", "INPUT"] and "!" not in rule and
                rule.count("-i") == 1 and rule.index("-i") + 1 < len(rule),
                "field_input_drop_shadowed: " + str(rule))
        iface = rule[rule.index("-i") + 1]
        require(iface != field_if and "+" not in iface,
                "field_input_drop_shadowed: " + str(rule))
    raise ValueError("field_input_drop_missing")


def assert_field_routes(snapshot, ot_if):
    def via(rows, dst, source=False):
        require(len(rows) == 1, "route_count: " + dst)
        row = rows[0]
        require(row.get("dst") == dst and row.get("gateway") == GATEWAY and
                row.get("dev") == ot_if and not row.get("nexthops") and
                row.get("type", "unicast") == "unicast", "route_wrong: " + str(row))
        if source:
            require(row.get("prefsrc") == OT, "route_source_wrong: " + str(row))
    via(snapshot["field_route"], "10.40.40.0/24")
    via(snapshot["default_route"], "default")
    for last in (20, 21, 22, 23):
        address = "10.40.40." + str(last)
        via(snapshot["route_get"][address], address, source=True)


def assert_host(snapshot):
    interfaces = assert_interfaces(snapshot["addresses"])
    assert_forwarding(snapshot["forwarding"], snapshot["addresses"])
    assert_forward_chain(snapshot["forward_chain"])
    assert_field_input(snapshot["input_chain"], interfaces[FIELD]["ifname"])
    assert_field_routes(snapshot, interfaces[OT]["ifname"])
    return interfaces


def checksum(data):
    if len(data) % 2:
        data += b"\0"
    total = sum(struct.unpack("!%dH" % (len(data) // 2), data))
    while total >> 16:
        total = (total & 65535) + (total >> 16)
    return (~total) & 65535


def mac_bytes(value):
    result = bytes.fromhex(value.replace(":", ""))
    require(len(result) == 6, "invalid_mac")
    return result


def make_frame(src_ip, src_mac, dst_mac, token, stage, seq):
    payload = ("RD-G4:" + token + ":" + stage + ":" + str(seq)).encode("ascii")
    src, dst = socket.inet_aton(src_ip), socket.inet_aton(DEST)
    udp = struct.pack("!HHHH", 49001, 49000, 8 + len(payload), 0) + payload
    pseudo = src + dst + struct.pack("!BBH", 0, 17, len(udp))
    check = checksum(pseudo + udp) or 65535
    udp = udp[:6] + struct.pack("!H", check) + udp[8:]
    ip = struct.pack("!BBHHHBBH4s4s", 0x45, 0, 20 + len(udp), seq,
                     0x4000, 64, 17, 0, src, dst)
    ip = ip[:10] + struct.pack("!H", checksum(ip)) + ip[12:]
    return mac_bytes(dst_mac) + mac_bytes(src_mac) + b"\x08\x00" + ip + udp


def decode_frame(frame, token):
    require(len(frame) >= 42 and frame[12:14] == b"\x08\x00", "invalid_ethernet")
    ip = frame[14:34]
    version, _, length, _, flags, ttl, proto, _, src, dst = struct.unpack(
        "!BBHHHBBH4s4s", ip)
    require(version == 0x45 and checksum(ip) == 0, "invalid_ip_checksum_or_header")
    require(flags & 0x3fff == 0 and 0 < ttl <= 64 and proto == 17,
            "invalid_ip_fragment_ttl_or_protocol")
    require(28 <= length <= len(frame) - 14, "invalid_ip_length")
    udp = frame[34:14 + length]
    sport, dport, size, check = struct.unpack("!HHHH", udp[:8])
    require(sport == 49001 and dport == 49000 and size == len(udp) and check != 0,
            "invalid_udp_header")
    pseudo = src + dst + struct.pack("!BBH", 0, 17, len(udp))
    require(checksum(pseudo + udp) == 0, "invalid_udp_checksum")
    payload = udp[8:].decode("ascii")
    parts = payload.split(":")
    require(len(parts) == 4 and parts[:2] == ["RD-G4", token] and
            parts[2] in ("pre", "neg", "post") and parts[3] in ("0", "1", "2"),
            "invalid_tag")
    return {"src": socket.inet_ntoa(src), "dst": socket.inet_ntoa(dst),
            "src_mac": frame[6:12].hex(), "dst_mac": frame[:6].hex(),
            "stage": parts[2], "seq": int(parts[3]), "ttl": ttl,
            "ip_checksum_valid": True, "udp_checksum_valid": True}


def emit(event, **fields):
    print(json.dumps(dict(event=event, time=stamp(), **fields)), flush=True)


def container_command(*args):
    try:
        result = subprocess.run(args, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                universal_newlines=True, timeout=3, check=False)
    except subprocess.TimeoutExpired as exc:
        print(json.dumps({"argv": args, "error": "timeout",
                          "stdout": (exc.stdout or b"").decode(errors="replace"),
                          "stderr": (exc.stderr or b"").decode(errors="replace")}),
              file=sys.stderr, flush=True)
        raise
    print(json.dumps({"argv": args, "stdout": result.stdout, "stderr": result.stderr,
                      "exit_code": result.returncode, "time": stamp()}),
          file=sys.stderr, flush=True)
    require(result.returncode == 0, "container_tool_error: " + str(args))
    return result.stdout


def helper_addresses():
    # IPv4-filtered addr JSON omits link-layer fields. Read MACs only from
    # link data, joined by identity rather than interface enumeration order.
    rows = json.loads(container_command("ip", "-j", "-4", "addr", "show"))
    link_rows = json.loads(container_command("ip", "-j", "link", "show"))
    result = []
    for row in rows:
        require(row.get("ifindex") and row.get("ifname"), "interface_identity_missing")
        matches = [link for link in link_rows if link.get("ifindex") == row["ifindex"]
                   and link.get("ifname") == row["ifname"]]
        require(len(matches) == 1, "interface_link_missing_or_duplicate: " + row["ifname"])
        result.append(dict(row, address=matches[0].get("address")))
    return result


def helper_snapshot():
    def ip(*args):
        return json.loads(container_command("ip", "-j", "-4", *args))
    rows = helper_addresses()
    paths = ["/proc/sys/net/ipv4/ip_forward"] + sorted(
        glob.glob("/proc/sys/net/ipv4/conf/*/forwarding"))
    values = {path: Path(path).read_text().strip() for path in paths}
    return {"time": stamp(), "addresses": rows, "forwarding": values,
            "forward_chain": container_command("iptables", "-S", "FORWARD"),
            "input_chain": container_command("iptables", "-S", "INPUT"),
            "field_route": ip("route", "show", "table", "main", "exact", "10.40.40.0/24"),
            "default_route": ip("route", "show", "default"),
            "route_get": {"10.40.40." + str(i): ip("route", "get", "10.40.40." + str(i))
                          for i in (20, 21, 22, 23)}}


def helper(mode, cfg):
    if mode == "snapshot":
        print(json.dumps(helper_snapshot()))
    elif mode == "links":
        print(json.dumps(helper_addresses()))
    elif mode == "observe":
        # Bound to one discovered interface. No promiscuous mode is needed.
        with socket.socket(socket.AF_PACKET, socket.SOCK_RAW, socket.htons(0x0800)) as sock:
            sock.bind((cfg["iface"], 0))
            sock.settimeout(0.2)
            deadline = time.monotonic() + 18
            emit("ready", iface=cfg["iface"])
            while time.monotonic() < deadline:
                try:
                    frame, address = sock.recvfrom(65535)
                except socket.timeout:
                    continue
                if address[2] != 0 or ("RD-G4:" + cfg["token"]).encode() not in frame:
                    continue
                fields = {"hex": frame.hex(), "packet_type": address[2]}
                try:
                    fields["decoded"] = decode_frame(frame, cfg["token"])
                except (ValueError, UnicodeError, struct.error) as exc:
                    fields["error"] = str(exc)
                emit("frame", **fields)
            # Linux SOL_PACKET / PACKET_STATISTICS (struct tpacket_stats).
            # A capture that overflowed cannot establish absence.
            packets, dropped = struct.unpack("=II", sock.getsockopt(263, 6, 8))
            emit("done", packets=packets, dropped=dropped)
    elif mode == "send":
        with socket.socket(socket.AF_PACKET, socket.SOCK_RAW, socket.htons(0x0800)) as sock:
            sock.bind((cfg["iface"], 0))
            sock.settimeout(1)
            for seq in range(3 if cfg["stage"] == "neg" else 1):
                frame = make_frame(cfg["src"], cfg["src_mac"], cfg["dst_mac"],
                                   cfg["token"], cfg["stage"], seq)
                require(sock.send(frame) == len(frame), "short_frame_send")
                emit("sent", hex=frame.hex(), decoded=decode_frame(frame, cfg["token"]))
                time.sleep(0.1)
    else:
        raise ValueError("unknown_helper_mode")


def frames(events, stage):
    return [e for e in events if e.get("event") == "frame" and
            e.get("decoded", {}).get("stage") == stage]


def assert_transit_evidence(ingress, arrivals, sent, token, links):
    for events in (ingress, arrivals):
        require(events and events[0]["event"] == "ready" and
                events[-1]["event"] == "done", "observer_not_ready_or_incomplete")
        require(events[-1].get("dropped") == 0, "observer_dropped_or_missing_statistics")
        require(all("error" not in event for event in events), "invalid_captured_frame")
        times = [event["time"]["monotonic"] for event in events]
        require(times == sorted(times), "observer_time_out_of_order")
    require(ingress[0].get("iface") == links[OT]["ifname"] and
            arrivals[0].get("iface") == links[DEST]["ifname"], "wrong_observer_interface")
    require(not frames(arrivals, "neg"), "transit_exposure: negative frame reached OpenDSS")
    require(len([e for e in ingress if e["event"] == "frame"]) == 3,
            "ingress_count_not_three")
    require(len([e for e in arrivals if e["event"] == "frame"]) == 2,
            "positive_control_count_not_two")
    require(len(sent) == 5, "send_count_not_five")
    for stage, count, observed in (("pre", 1, arrivals), ("neg", 3, ingress),
                                   ("post", 1, arrivals)):
        actual = frames(observed, stage)
        emitted = [e for e in sent if e["decoded"]["stage"] == stage]
        require(len(actual) == len(emitted) == count and
                sorted(e["decoded"]["seq"] for e in actual) == list(range(count)),
                "missing_positive_control_or_ingress: " + stage)
        for event in actual + emitted:
            decoded = decode_frame(bytes.fromhex(event["hex"]), token)
            require(decoded == event["decoded"], "decoded_evidence_mismatch")
            src = SOURCE if stage == "neg" else PHYSICS
            src_mac = links[src]["mac"].replace(":", "")
            dst_mac = links[OT if stage == "neg" else DEST]["mac"].replace(":", "")
            require(decoded["src"] == src and decoded["dst"] == DEST and
                    decoded["src_mac"] == src_mac and decoded["dst_mac"] == dst_mac and
                    decoded["ttl"] == 64, "frame_address_or_ttl_mismatch")
        require(all(event.get("packet_type") == 0 for event in actual),
                "frame_not_ingress")
        require({e["hex"] for e in actual} == {e["hex"] for e in emitted},
                "sent_ingress_frame_mismatch")
    pre, post = frames(arrivals, "pre")[0], frames(arrivals, "post")[0]
    # These timestamps all come from OpenDSS, not the host clock.
    require(pre["time"]["monotonic"] < post["time"]["monotonic"] <
            arrivals[-1]["time"]["monotonic"], "positive_control_order")
    # Containers in this range use the same Linux kernel monotonic clock.
    # Both the datagrams sent and the actual ingress must be bracketed by
    # OpenDSS's controls. Host time is used only for process deadlines.
    negative = frames(ingress, "neg") + [e for e in sent if e["decoded"]["stage"] == "neg"]
    require(all(pre["time"]["monotonic"] < e["time"]["monotonic"] <
                post["time"]["monotonic"] for e in negative), "negative_outside_control_window")


class Evidence:
    def __init__(self, directory, token):
        self.directory = Path(directory) / token
        self.directory.mkdir(parents=True, exist_ok=False)
        self.record = {"gate": "us-rtac", "token": token, "result": "FAIL",
                       "scope": "IPv4 OT Ops -> physics only", "events": []}
        self.save()

    def save(self):
        (self.directory / "us-rtac.json").write_text(json.dumps(self.record, indent=2) + "\n")

    def event(self, kind, **fields):
        self.record["events"].append(dict(sequence=len(self.record["events"]),
                                          kind=kind, **fields))
        self.save()

    def raw(self, name, data):
        path = self.directory / name
        path.write_bytes(data if isinstance(data, bytes) else data.encode())
        return str(path)


class Transport:
    """All acting calls live here; tests inject offline transports."""
    def __init__(self, evidence, api):
        self.evidence, self.api = evidence, api.rstrip("/")
        self.source = Path(__file__).read_text()
        self.collectors = []
        self.serial = 0

    def argv(self, container, mode, cfg):
        return ["docker", "exec", "-i", container, "python3", "-u", "-c",
                self.source, "--helper", mode, json.dumps(cfg)]

    def command(self, container, mode, cfg=None):
        self.serial += 1
        name = "%02d-%s-%s" % (self.serial, container, mode)
        try:
            result = subprocess.run(self.argv(container, mode, cfg or {}),
                                    stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                    timeout=12, check=False)
        except subprocess.TimeoutExpired as exc:
            self.evidence.raw(name + ".stdout", exc.stdout or b"")
            self.evidence.raw(name + ".stderr", exc.stderr or b"")
            self.evidence.event("command", container=container, mode=mode, error="timeout")
            raise ValueError("tool_timeout: " + container + " " + mode) from exc
        except OSError as exc:
            self.evidence.event("command", container=container, mode=mode, error=str(exc))
            raise ValueError("tool_unavailable: " + str(exc)) from exc
        self.evidence.raw(name + ".stdout", result.stdout)
        self.evidence.raw(name + ".stderr", result.stderr)
        self.evidence.event("command", container=container, mode=mode,
                            config=cfg, exit_code=result.returncode, raw=name)
        require(result.returncode == 0, "tool_error: " + name)
        text = result.stdout.decode()
        return [json.loads(line) for line in text.splitlines()] if mode == "send" else json.loads(text)

    def http(self, path, data=None):
        self.serial += 1
        name = "%02d-http" % self.serial
        request = urllib.request.Request(self.api + path, data=(
            json.dumps(data).encode() if data is not None else None),
            headers={"Content-Type": "application/json"})
        try:
            with urllib.request.urlopen(request, timeout=5) as response:
                body, code = response.read(1024 * 1024), response.status
        except urllib.error.HTTPError as exc:
            with exc:
                body, code = exc.read(1024 * 1024), exc.code
        except (OSError, urllib.error.URLError) as exc:
            self.evidence.event("http", path=path, request=data, error=str(exc))
            raise ValueError("http_transport_error: " + str(exc)) from exc
        self.evidence.raw(name + ".body", body)
        self.evidence.event("http", path=path, request=data, status=code, raw=name)
        require(code == 200, "http_status_%s: %s (no retry)" % (code, path))
        return json.loads(body)

    def observe(self, container, iface, token):
        path = self.evidence.directory / (container + "-capture.jsonl")
        out = path.open("wb")
        err = (self.evidence.directory / (container + "-capture.stderr")).open("wb")
        try:
            proc = subprocess.Popen(self.argv(container, "observe",
                                             {"iface": iface, "token": token}),
                                    stdout=out, stderr=err)
        except BaseException:
            out.close()
            err.close()
            raise
        collector = {"process": proc, "path": path, "out": out, "err": err}
        self.collectors.append(collector)
        self.evidence.event("observer_started", container=container, iface=iface, raw=path.name)
        return collector

    def events(self, collector):
        # A concurrent last line may not yet be complete.
        lines = collector["path"].read_bytes().split(b"\n")[:-1]
        return [json.loads(line) for line in lines]

    def wait_event(self, collector, predicate):
        deadline = time.monotonic() + 4
        while time.monotonic() < deadline:
            events = self.events(collector)
            require(not any("error" in e for e in events), "invalid_captured_frame")
            if predicate(events):
                return events
            require(collector["process"].poll() is None, "observer_exited_early")
            time.sleep(0.05)
        raise ValueError("observer_readiness_or_frame_timeout")

    def finish(self, collector):
        try:
            code = collector["process"].wait(timeout=22)
        except subprocess.TimeoutExpired as exc:
            raise ValueError("observer_finish_timeout") from exc
        require(code == 0, "observer_tool_error: " + collector["path"].name)
        return self.events(collector)

    def cleanup(self):
        errors = []
        for collector in self.collectors:
            proc = collector["process"]
            try:
                # Let the bounded in-container observer exit itself. Killing
                # docker exec alone would not guarantee the remote child died.
                code = proc.wait(timeout=22)
                if code != 0:
                    errors.append("observer_exit_" + str(code))
            except subprocess.TimeoutExpired:
                proc.kill()
                proc.wait(timeout=2)
                errors.append("observer_cleanup_timeout")
            finally:
                collector["out"].close()
                collector["err"].close()
        require(not errors, "cleanup_failed: " + str(errors))


class Gate:
    def __init__(self, transport, evidence, token):
        self.transport, self.evidence, self.token = transport, evidence, token

    def probe(self, links):
        t = self.transport
        links = dict(links)
        links.update(assert_interfaces(t.command(PLC, "links"), (SOURCE,)))
        links.update(assert_interfaces(t.command(DSS, "links"), (DEST,)))
        self.evidence.event("probe_links", links=links)
        ingress = t.observe(RTAC, links[OT]["ifname"], self.token)
        arrivals = t.observe(DSS, links[DEST]["ifname"], self.token)
        for collector in (ingress, arrivals):
            t.wait_event(collector, lambda events: bool(events) and events[0]["event"] == "ready")

        def send(stage):
            src = SOURCE if stage == "neg" else PHYSICS
            return t.command(PLC if stage == "neg" else RTAC, "send", {
                "iface": links[src]["ifname"], "src": src, "src_mac": links[src]["mac"],
                "dst_mac": links[OT if stage == "neg" else DEST]["mac"],
                "token": self.token, "stage": stage})

        sent = send("pre")
        t.wait_event(arrivals, lambda events: len(frames(events, "pre")) == 1)
        sent += send("neg")
        t.wait_event(ingress, lambda events: len(frames(events, "neg")) == 3)
        # Observe the complete negative window, including a late arrival after
        # the post control. No reply or application listener is used as an oracle.
        sent += send("post")
        t.wait_event(arrivals, lambda events: len(frames(events, "post")) == 1)
        observed_ingress, observed_arrivals = t.finish(ingress), t.finish(arrivals)
        assert_transit_evidence(observed_ingress, observed_arrivals, sent, self.token, links)
        self.evidence.event("transit_proven", sent=5, ingress=3, negative_arrivals=0,
                            positive_controls=2, ingress_window=observed_ingress,
                            destination_window=observed_arrivals)

    def run(self):
        # Only the shared backend receipt is authoritative. Never recommit or
        # poll for a different apply identity to hide a failed receipt.
        from containd_readiness import require_backend_protection
        errors = []
        ready = False
        try:
            status = self.transport.http("/api/range")
            require(status.get("package") == "us-dnp3-substation" and
                    status.get("phase") == "ready", "wrong_or_unready_range")
            ready = True
            require_backend_protection(status)
            before = self.transport.command(RTAC, "snapshot")
            links = assert_host(before)
            self.evidence.event("host_before_proven", snapshot=before)
            try:
                self.probe(links)
            except Exception as exc:
                self.evidence.event("probe_failure", message=str(exc))
                raise
            finally:
                after = self.transport.command(RTAC, "snapshot")
                require(assert_host(after) == links, "rtac_interfaces_changed")
                self.evidence.event("host_after_proven", snapshot=after)
        except Exception as exc:
            errors.append(str(exc))
            self.evidence.event("failure", message=str(exc))
        finally:
            try:
                self.transport.cleanup()
                self.evidence.event("observers_cleaned")
            except Exception as exc:
                errors.append(str(exc))
                self.evidence.event("cleanup_failure", message=str(exc))
            # Never change a different package's policy on a failed preflight.
            if ready:
                try:
                    response = self.transport.http("/api/firewall/apply", {"config": "weak"})
                    receipt = require_backend_protection(response)
                    self.evidence.event("weak_restored", receipt=receipt)
                except Exception as exc:
                    errors.append("weak_restore_failed: " + str(exc))
                    self.evidence.event("restore_failure", message=str(exc))
            else:
                errors.append("weak_restore_not_safe: US range readiness not proven")
        self.evidence.record.update(result="FAIL" if errors else "PASS", errors=errors)
        self.evidence.save()
        return 1 if errors else 0


def main():
    if len(sys.argv) > 1 and sys.argv[1] == "--helper":
        helper(sys.argv[2], json.loads(sys.argv[3]))
        return 0
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--api", default=os.environ.get("API", "http://localhost:8088"))
    parser.add_argument("--evidence-dir", default="build/smoke-receipts/us-rtac")
    args = parser.parse_args()
    token = uuid.uuid4().hex
    evidence = Evidence(args.evidence_dir, token)
    code = Gate(Transport(evidence, args.api), evidence, token).run()
    for error in evidence.record.get("errors", []):
        print(error, file=sys.stderr)
    print("us-rtac-smoke %s: %s" % (evidence.record["result"], evidence.directory))
    return code


if __name__ == "__main__":
    sys.exit(main())
