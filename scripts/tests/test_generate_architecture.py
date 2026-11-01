"""Offline contracts for architecture generation; fixtures derive from the tree."""

from contextlib import redirect_stderr
import io
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

from scripts import generate_architecture as architecture

ROOT = Path(__file__).resolve().parents[2]


class ArchitectureTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.rendered = architecture.render(ROOT)
        cls.packages, cls.overlay = architecture.load(ROOT)
        cls.us = next(p for p in cls.packages if p.short == "us")
        cls.eu = next(p for p in cls.packages if p.short == "eu")

    def test_deterministic_and_repository_is_current(self):
        self.assertEqual(self.rendered, architecture.render(ROOT))
        self.assertEqual(architecture.generate(ROOT, check=True), 0)

    def test_package_pointers_not_guessed_names(self):
        self.assertEqual(self.us.topology_path, ROOT / "lab-definitions/substation-segmentation.yml")
        self.assertEqual(self.eu.topology_path, self.eu.directory / "topology.yml")
        self.assertIn("OT Operations Zone", self.rendered["network-us"])
        self.assertIn("Control Centre Zone", self.rendered["network-eu"])

    def test_source_and_release_are_distinct(self):
        for key in ("compose-us", "compose-eu"):
            graph = self.rendered[key]
            self.assertIn("source: build build/Dockerfile.mgmt", graph)
            self.assertIn("https://github.com/tonylturner/containd.git", graph)
            self.assertIn("tag: rangerdanger-firewall", graph)
            self.assertIn("release: pull ghcr.io/tonylturner/containd:latest", graph)

    def test_dependencies_come_from_compose_and_keep_conditions(self):
        graph = self.rendered["compose-us"]
        node = lambda key: architecture.ident("us", "compose", key)
        self.assertIn(architecture.edge(node("historian_sim"), node("rtac_sim"), "service_started"), graph)
        self.assertIn(architecture.edge(node("hmi_poller"), node("rtac_sim"), "service_started"), graph)
        self.assertIn(architecture.edge(node("rtac_sim"), node("relay_sim"), "service_healthy", "-.->"), graph)
        for condition in ("service_started", "service_healthy"):
            self.assertNotIn(architecture.edge(node("rtac_sim"), node("openplc"), condition), graph)
        self.assertIn("lifecycle start, routes, seed policy", graph)

    def test_namespace_sharing_is_not_a_new_nic(self):
        self.assertIn("shares network namespace", self.rendered["compose-us"])
        self.assertEqual(self.rendered["network-us"].count("shares fuxa_hmi namespace"), 2)
        self.assertNotIn("hmi_poller", self.rendered["network-eu"])

    def test_external_networks_join_platform_by_name(self):
        self.assertTrue(self.us.networks["desk_corp_net"]["internal"])
        self.assertIn("10.98.1.0/28", self.rendered["network-us"])
        # The EU range does not use corporate delivery, but its platform proxy
        # still has that NIC. Do not erase real wiring from the complete graph.
        self.assertIn("desk_corp_net", self.rendered["network-eu"])
        self.assertNotIn("corp_ws", self.rendered["network-eu"])

    def test_overview_routes_and_logical_edges_use_the_correct_nics(self):
        nic = lambda network, service: architecture.ident("us", "nic", network, service)
        graph = self.rendered["overview"]
        self.assertIn(architecture.edge(nic("mgmt_net", "backend"), architecture.ident("dockerd"),
                                       "Docker SDK via /var/run/docker.sock", "-.->"), graph)
        self.assertIn(architecture.edge(nic("mgmt_net", "backend"), nic("mgmt_net", "firewall"),
                                       "JWT REST · policy / PCAP / events", "-.->"), graph)
        self.assertIn(architecture.edge(nic("physics_net", "rtac_sim"), nic("physics_net", "opendss_sim"),
                                       "HTTP push · /api/update-state", "-.->"), graph)
        self.assertIn('frontend:3000<br/>/', graph)
        self.assertIn('corp_ws:3000<br/>/apps/corp-ws/', graph)

    def test_nginx_api_exceptions_and_app_upstreams(self):
        routes = architecture.routes(ROOT, self.us)
        fuxa = routes[("proxy", "fuxa_hmi:1881")]
        for path in ("version", "settings", "project", "projectData", "device", "alarmsHistory", "resources"):
            self.assertIn("/api/" + path, fuxa)
        self.assertIn("/apps/openplc/", routes[("proxy", "openplc:8080")])
        self.assertIn("/containd/", routes[("proxy", "firewall:8080")])
        self.assertNotIn("/apps/firewall", self.rendered["request-flow"])
        self.assertNotIn(("proxy", "fuxa_hmi:1881"), architecture.routes(ROOT, self.eu))
        self.assertIn("except package-specific nginx locations", self.rendered["request-flow"])
        self.assertIn("403 response", self.rendered["request-flow"])

    def test_terminal_targets_are_only_manifest_nodes(self):
        self.assertIn("Terminal targets (manifest nodes only)", self.rendered["request-flow"])
        for package in self.packages:
            targets = [s["key"] for s in package.nodes.values() if s.get("node")]
            self.assertNotIn("opendss_sim", targets)
            self.assertNotIn("hmi_poller", targets)
            for key in targets:
                node = package.nodes[key]["node"]
                self.assertIn(f"{node} → {key}", self.rendered["request-flow"])

    def test_labels_escape_untrusted_mermaid_and_html_characters(self):
        text = architecture.label('a"<b>&[#]`|\\\nsecond')
        self.assertEqual(text, "a#34;#60;b#62;#38;#91;#35;#93;#96;#124;#92;<br/>second")
        self.assertNotEqual(architecture.ident("a-b"), architecture.ident("a_b"))
        self.assertNotEqual(architecture.ident("a", "b"), architecture.ident("a_b"))

    def test_nginx_parser_keeps_quoted_braces_and_rejects_broken_input(self):
        self.assertEqual(architecture.nginx_directives('a "#{};"; # ignored\nx y;'),
                         [(["a", "#{};"], None), (["x", "y"], None)])
        self.assertEqual(architecture.nginx_directives('a "{" ";" "}";'),
                         [(["a", "{", ";", "}"], None)])
        for text in ("a {", "}", "a b", 'a "unfinished', "a { x }"):
            with self.subTest(text=text), self.assertRaises(ValueError):
                architecture.nginx_directives(text)

    def test_markers_reject_missing_duplicate_unbalanced_and_unknown(self):
        begin, end = "<!-- BEGIN GENERATED: x -->", "<!-- END GENERATED: x -->"
        for text in ("", begin, end + begin, begin + end + begin + end,
                     begin + begin + end + end, begin + "<!-- END GENERATED: y -->",
                     "<!-- BEGIN GENERATED x -->" + end):
            with self.subTest(text=text), self.assertRaises(ValueError):
                architecture.replace_regions(text, ("x",), {"x": "new"})

    def test_replacement_preserves_prose_and_is_idempotent(self):
        begin, end = "<!-- BEGIN GENERATED: x -->", "<!-- END GENERATED: x -->"
        source = "before\n" + begin + "old" + end + "\nafter"
        updated, changed = architecture.replace_regions(source, ("x",), {"x": "new"})
        self.assertEqual(updated, "before\n" + begin + "\nnew\n" + end + "\nafter")
        self.assertEqual(changed, ["x"])
        self.assertEqual(architecture.replace_regions(updated, ("x",), {"x": "new"}), (updated, []))

    def fixture(self, root):
        # Copy only the generator's declared inputs; never a worktree, caches or data.
        for relative in ("lab-definitions", "docs/architecture", "proxy"):
            shutil.copytree(ROOT / relative, root / relative)
        for relative in ("README.md", "docs/architecture-diagram.md", "docker-compose.yml",
                         "docker-compose.release.yml", "frontend/package.json", ".github/release-images.json",
                         "scripts/generate_architecture.py"):
            path = root / relative
            path.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(ROOT / relative, path)
        for item in self.overlay["edges"]:
            relative = item["source"].rsplit(":", 1)[0]
            path = root / relative
            path.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(ROOT / relative, path)

    def test_subnet_mutation_cli_check_fails_without_writing(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            self.fixture(root)
            # Derive the mutation from Compose, not a hardcoded future authority.
            import yaml
            for mode in ("source", "release"):
                path = root / "lab-definitions/packages/us-dnp3-substation" / f"compose.{mode}.yml"
                data = yaml.safe_load(path.read_text())
                subnet = data["networks"]["enterprise_net"]["ipam"]["config"][0]["subnet"]
                host, prefix = subnet.split("/")
                changed = f"{host}/{int(prefix) - 1}"
                path.write_text(path.read_text().replace(subnet, changed))
            before = {p: p.read_bytes() for p in root.rglob("*") if p.is_file()}
            result = subprocess.run([sys.executable, str(root / "scripts/generate_architecture.py"), "--check"],
                                    capture_output=True, text=True, timeout=30)
            self.assertEqual(result.returncode, 1, result.stderr)
            self.assertIn("network-us", result.stderr)
            self.assertIn("overview", result.stderr)
            self.assertEqual(before, {p: p.read_bytes() for p in root.rglob("*") if p.is_file()})

    def test_one_mode_subnet_mutation_is_rejected_not_hidden(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            self.fixture(root)
            path = root / "lab-definitions/packages/us-dnp3-substation/compose.source.yml"
            subnet = self.us.source["networks"]["enterprise_net"]["ipam"]["config"][0]["subnet"]
            host, prefix = subnet.split("/")
            path.write_text(path.read_text().replace(subnet, f"{host}/{int(prefix) - 1}"))
            result = subprocess.run([sys.executable, str(root / "scripts/generate_architecture.py"), "--check"],
                                    capture_output=True, text=True, timeout=30)
            self.assertEqual(result.returncode, 1)
            self.assertIn("source/release wiring differs: us-dnp3-substation", result.stderr)

    def test_missing_overlay_citation_fails(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            self.fixture(root)
            (root / self.overlay["edges"][0]["source"].rsplit(":", 1)[0]).unlink()
            with self.assertRaisesRegex(ValueError, "overlay source does not exist"):
                architecture.render(root)

    def test_marker_error_does_not_partially_write_other_file(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for relative in architecture.REGIONS:
                path = root / relative
                path.parent.mkdir(parents=True, exist_ok=True)
                shutil.copyfile(ROOT / relative, path)
            path = root / "docs/architecture-diagram.md"
            path.write_text(path.read_text().replace("END GENERATED: compose-us", "END GENERATED: unknown"))
            before = (root / "README.md").read_bytes()
            with patch.object(architecture, "render", return_value={**self.rendered, "overview": "changed"}):
                with self.assertRaisesRegex(ValueError, "unknown"):
                    architecture.generate(root)
            self.assertEqual((root / "README.md").read_bytes(), before)

    def test_unknown_proxy_variable_fails(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            self.fixture(root)
            path = root / "lab-definitions/packages/us-dnp3-substation/nginx.routes.conf"
            path.write_text(path.read_text().replace("proxy_pass $u;", "proxy_pass $unknown;", 1))
            with self.assertRaisesRegex(ValueError, "unresolved nginx variable"):
                architecture.render(root)

    def test_stale_region_check_names_drift_and_writes_nothing(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            self.fixture(root)
            path = root / "README.md"
            path.write_text(path.read_text().replace("flowchart LR", "flowchart TB", 1))
            before = path.read_bytes()
            stderr = io.StringIO()
            with redirect_stderr(stderr):
                self.assertEqual(architecture.generate(root, check=True), 1)
            self.assertIn("README.md: overview", stderr.getvalue())
            self.assertEqual(path.read_bytes(), before)
            self.assertEqual(architecture.generate(root), 0)
            self.assertEqual(architecture.generate(root, check=True), 0)


if __name__ == "__main__":
    unittest.main()
