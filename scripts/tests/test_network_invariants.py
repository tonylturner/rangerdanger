"""Frozen network values: compose networking changes only through one reviewed fixture.

Parses the platform and package compose files as YAML (no Docker) and compares every
network value against scripts/tests/fixtures/network-invariants.json.
"""
from __future__ import annotations

import json
import re
import unittest
from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parents[2]
FIXTURE = ROOT / "scripts/tests/fixtures/network-invariants.json"
PACKAGES = ROOT / "lab-definitions/packages"
FROZEN = (
    "Networking is frozen: change scripts/tests/fixtures/network-invariants.json only "
    "when Tony asked for this exact change, and run `scripts/firewall-smoke.sh both` "
    "against a live range."
)
# Service keys whose whole value is network behaviour; anything else is out of scope.
SERVICE_NETWORK_KEYS = (
    "networks", "network_mode", "ports", "sysctls",
    "dns", "dns_opt", "dns_search", "extra_hosts", "mac_address",
)
FROZEN_ENV = re.compile(r"^(GATEWAY|CONTAIND_MGMT_RESERVATION|CONTAIND_AUTO_[A-Z0-9]+_SUBNET)$")
LOOPBACK = "127.0.0.1"


def stacks() -> dict[str, tuple[str, str]]:
    """Map each stack to its (source, release) compose paths relative to ROOT."""
    found = {"platform": ("docker-compose.yml", "docker-compose.release.yml")}
    for package in sorted(p for p in PACKAGES.iterdir() if p.is_dir()):
        base = package.relative_to(ROOT).as_posix()
        found[package.name] = (f"{base}/compose.source.yml", f"{base}/compose.release.yml")
    return found


def load(name: str) -> dict:
    return yaml.safe_load((ROOT / name).read_text())


def environment(service: dict) -> dict[str, str]:
    env = service.get("environment") or {}
    if isinstance(env, list):
        env = dict(item.split("=", 1) if "=" in item else (item, None) for item in env)
    return {key: value for key, value in env.items() if FROZEN_ENV.match(key)}


def flatten(prefix: str, value, out: dict) -> None:
    if isinstance(value, dict) and value:
        for key, item in value.items():
            flatten(f"{prefix}/{key}", item, out)
    elif isinstance(value, list) and value:
        for index, item in enumerate(value):
            flatten(f"{prefix}/{index}", item, out)
    else:
        # Empty mappings stay as leaves so a bare network attachment is still recorded.
        out[prefix] = value


def attachments(service: dict) -> dict:
    networks = service.get("networks")
    if isinstance(networks, list):
        return {name: {} for name in networks}
    return {name: attachment or {} for name, attachment in (networks or {}).items()}


def extract(doc: dict) -> dict:
    """Flatten every frozen network value of one compose document to path -> value."""
    out: dict = {}
    for name, network in (doc.get("networks") or {}).items():
        flatten(f"networks/{name}", network or {}, out)
    for name, service in doc["services"].items():
        for key in SERVICE_NETWORK_KEYS:
            if key == "networks" and key in service:
                flatten(f"services/{name}/networks", attachments(service), out)
            elif key in service:
                flatten(f"services/{name}/{key}", service[key], out)
        for key, value in environment(service).items():
            out[f"services/{name}/environment/{key}"] = value
    return out


def port_host_ip(port) -> str | None:
    if isinstance(port, dict):
        return port.get("host_ip")
    parts = str(port).split("/", 1)[0].split(":")
    return parts[0] if len(parts) == 3 else None


def rule_violations(doc: dict, name: str) -> list[str]:
    """Hard rules that hold whatever the fixture says."""
    problems = []
    for service_name, service in doc["services"].items():
        where = f"{name}: services/{service_name}"
        if "networks" not in service and "network_mode" not in service:
            problems.append(f"{where} declares neither networks nor network_mode "
                            "(it would join an implicit default network)")
        if "env_file" in service:
            problems.append(f"{where} uses env_file; GATEWAY and containd network env "
                            "must stay inline in the compose file")
        for network, attachment in attachments(service).items():
            for key in ("gw_priority", "priority"):
                if key in attachment:
                    problems.append(f"{where}/networks/{network} sets {key}; default routes "
                                    "come only from the GATEWAY env via set-gateway.sh")
        for port in service.get("ports") or []:
            if port_host_ip(port) != LOOPBACK:
                problems.append(f"{where}/ports binds {port!r}; every host port must bind "
                                f"{LOOPBACK}")
    return problems


def differences(expected: dict, actual: dict, name: str) -> list[str]:
    problems = []
    for path in sorted(expected.keys() | actual.keys()):
        if path not in actual:
            problems.append(f"{name}: {path} removed (frozen value {expected[path]!r})")
        elif path not in expected:
            problems.append(f"{name}: {path} added with {actual[path]!r}")
        elif expected[path] != actual[path]:
            problems.append(f"{name}: {path} changed from {expected[path]!r} "
                            f"to {actual[path]!r}")
    return problems


def report(problems: list[str]) -> str:
    return "\n".join([*problems, FROZEN])


class NetworkInvariants(unittest.TestCase):
    @classmethod
    def setUpClass(cls) -> None:
        cls.fixture = json.loads(FIXTURE.read_text())["stacks"]
        cls.stacks = stacks()
        cls.docs = {name: load(name) for pair in cls.stacks.values() for name in pair}

    def test_fixture_covers_every_stack(self) -> None:
        self.assertEqual(sorted(self.fixture), sorted(self.stacks), report([
            "the fixture's stacks differ from the platform plus lab-definitions/packages/*"]))

    def test_compose_matches_fixture(self) -> None:
        for stack, pair in self.stacks.items():
            for name in pair:
                with self.subTest(compose=name):
                    problems = differences(self.fixture.get(stack, {}),
                                           extract(self.docs[name]), name)
                    if problems:
                        self.fail(report(problems))

    def test_source_and_release_agree(self) -> None:
        for source, release in self.stacks.values():
            with self.subTest(stack=source):
                problems = differences(extract(self.docs[source]),
                                       extract(self.docs[release]), release)
                if problems:
                    self.fail(report([f"{release} disagrees with {source}:", *problems]))

    def test_hard_rules(self) -> None:
        for name, doc in self.docs.items():
            with self.subTest(compose=name):
                problems = rule_violations(doc, name)
                if problems:
                    self.fail(report(problems))


MODEL = """
services:
  firewall:
    ports: ["127.0.0.1:9080:8080", "127.0.0.1:9443:8443", "127.0.0.1:2222:2222"]
    environment:
      - CONTAIND_AUTO_LAN2_SUBNET=10.40.40.0/24
    networks:
      enterprise_net: {interface_name: wan, ipv4_address: 10.10.10.2}
      field_net: {interface_name: lan2, ipv4_address: 10.40.40.2}
  kali:
    environment: {GATEWAY: 10.10.10.2}
    networks:
      enterprise_net: {ipv4_address: 10.10.10.50}
  rtac_sim:
    sysctls: {net.ipv4.ip_forward: 0}
    networks:
      ot_ops_net: {ipv4_address: 10.30.30.20}
      mgmt_net: {ipv4_address: 10.99.99.20}
  relay_sim:
    environment: [GATEWAY=10.40.40.2]
    networks:
      field_net: {ipv4_address: 10.40.40.20}
  hmi_poller:
    network_mode: "service:kali"
networks:
  field_net:
    ipam:
      config: [{subnet: 10.40.40.0/24, gateway: 10.40.40.1}]
"""


class NegativeChecks(unittest.TestCase):
    """Each guarded change to a compose model, made in memory, must be reported."""

    NAME = "compose.yml"

    def setUp(self) -> None:
        self.frozen = extract(yaml.safe_load(MODEL))
        self.changed = yaml.safe_load(MODEL)

    def problems(self) -> list[str]:
        return (differences(self.frozen, extract(self.changed), self.NAME)
                + rule_violations(self.changed, self.NAME))

    def assert_reported(self, *fragments: str) -> None:
        message = "\n".join(self.problems())
        for fragment in fragments:
            self.assertIn(fragment, message)

    def test_unchanged_model_passes(self) -> None:
        self.assertEqual(self.problems(), [])

    def test_static_ip_change(self) -> None:
        self.changed["services"]["rtac_sim"]["networks"]["ot_ops_net"]["ipv4_address"] = "10.30.30.21"
        self.assert_reported("services/rtac_sim/networks/ot_ops_net/ipv4_address changed "
                             "from '10.30.30.20' to '10.30.30.21'")

    def test_interface_name_change(self) -> None:
        self.changed["services"]["firewall"]["networks"]["field_net"]["interface_name"] = "eth4"
        self.assert_reported("services/firewall/networks/field_net/interface_name changed")

    def test_gw_priority_on_attachment(self) -> None:
        self.changed["services"]["kali"]["networks"]["enterprise_net"]["gw_priority"] = 10
        self.assert_reported("services/kali/networks/enterprise_net sets gw_priority",
                             "services/kali/networks/enterprise_net/gw_priority added")

    def test_priority_on_attachment(self) -> None:
        self.changed["services"]["rtac_sim"]["networks"]["mgmt_net"]["priority"] = 1
        self.assert_reported("services/rtac_sim/networks/mgmt_net sets priority")

    def test_port_off_loopback(self) -> None:
        self.changed["services"]["firewall"]["ports"][0] = "0.0.0.0:9080:8080"
        self.assert_reported("services/firewall/ports binds '0.0.0.0:9080:8080'",
                             "services/firewall/ports/0 changed")

    def test_port_without_host_ip(self) -> None:
        self.changed["services"]["firewall"]["ports"][2] = "2222:2222"
        self.assert_reported("services/firewall/ports binds '2222:2222'")

    def test_gateway_env_change(self) -> None:
        self.changed["services"]["relay_sim"]["environment"] = ["GATEWAY=10.40.40.1"]
        self.changed["services"]["kali"]["environment"]["GATEWAY"] = "10.10.10.1"
        self.assert_reported("services/relay_sim/environment/GATEWAY changed "
                             "from '10.40.40.2' to '10.40.40.1'",
                             "services/kali/environment/GATEWAY changed")

    def test_containd_autobind_change(self) -> None:
        self.changed["services"]["firewall"]["environment"] = ["CONTAIND_AUTO_LAN2_SUBNET=10.40.0.0/16"]
        self.assert_reported("services/firewall/environment/CONTAIND_AUTO_LAN2_SUBNET changed")

    def test_network_mode_change(self) -> None:
        self.changed["services"]["hmi_poller"]["network_mode"] = "host"
        self.assert_reported("services/hmi_poller/network_mode changed")

    def test_new_network_attachment(self) -> None:
        self.changed["services"]["kali"]["networks"]["field_net"] = None
        self.assert_reported("services/kali/networks/field_net added with {}")

    def test_subnet_and_sysctl_change(self) -> None:
        self.changed["networks"]["field_net"]["ipam"]["config"][0]["subnet"] = "10.40.0.0/16"
        self.changed["services"]["rtac_sim"]["sysctls"]["net.ipv4.ip_forward"] = 1
        self.assert_reported("networks/field_net/ipam/config/0/subnet changed",
                             "services/rtac_sim/sysctls/net.ipv4.ip_forward changed from 0 to 1")

    def test_service_without_networks(self) -> None:
        self.changed["services"]["probe"] = {"image": "alpine:3.21"}
        self.assert_reported("services/probe declares neither networks nor network_mode")

    def test_report_names_the_rule(self) -> None:
        self.assertTrue(report(["x"]).endswith(FROZEN))
        self.assertIn("scripts/firewall-smoke.sh both", FROZEN)


if __name__ == "__main__":
    unittest.main()
