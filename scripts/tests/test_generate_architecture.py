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
        self.assertIn("OT Operations Zone", self.rendered["overview"])
        self.assertEqual(self.eu.zones["control_centre_net"]["name"], "Control Centre Zone")

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
        self.assertEqual(self.rendered["network-us"].count("hmi_poller shares namespace"), 1)
        self.assertNotIn("hmi_poller", self.rendered["network-eu"])

    def test_external_networks_join_platform_by_name(self):
        self.assertTrue(self.us.networks["desk_corp_net"]["internal"])
        self.assertIn("10.98.1.0/28", self.rendered["network-us"])
        # The EU range does not use corporate delivery, but its platform proxy
        # still has that NIC. Do not erase real wiring from the complete graph.
        self.assertIn("desk_corp_net", self.rendered["network-eu"])
        self.assertNotIn("corp_ws", self.rendered["network-eu"])

    def test_overview_has_one_service_per_primary_zone_and_logical_edges(self):
        node = lambda key: architecture.ident("overview", key)
        graph = self.rendered["overview"]
        self.assertIn(architecture.edge(node("backend"), node("dockerd"), "Docker SDK · socket", "-.->"), graph)
        self.assertIn(architecture.edge(node("backend"), node("firewall"), "JWT REST · management", "-.->"), graph)
        self.assertIn(architecture.edge(node("rtac_sim"), node("opendss_sim"), "HTTP push", "-.->"), graph)
        self.assertEqual(architecture.primary_network(self.us, "rtac_sim"), "ot_ops_net")
        self.assertEqual(architecture.primary_network(self.us, "corp_ws"), "enterprise_net")
        self.assertIn("Enterprise Zone ·<br/>10.10.10.0/24 · wan", graph)
        self.assertNotIn("10.99.99.10", graph)
        self.assertIn("10.98.1.0/28", graph)
        self.assertIn("fill:#fff7ed", graph)

    def test_aggregated_views_never_duplicate_services_or_identity_edges(self):
        import re
        for package in self.packages:
            graph = self.rendered[f"network-{package.short}"]
            self.assertNotIn("same ", graph)
            for key, spec in package.services.items():
                if spec.get("network_mode"):
                    self.assertNotIn(architecture.ident(package.short, "service", key), graph)
                    continue
                node = architecture.ident(package.short, "service", key)
                self.assertEqual(graph.count(node + '["'), 1, key)
                if len(spec["networks"]) > 1:
                    for network, nic in spec["networks"].items():
                        text = nic["ipv4_address"] + (" · " + nic["interface_name"] if nic.get("interface_name") else "")
                        self.assertIn(architecture.edge(node, architecture.ident(package.short, "network", network), text, "---"), graph)
            self.assertLessEqual(len(re.findall(r'^    \w+(?:\["|\(\()', graph, re.M)), 30)
        self.assertNotIn("same ", self.rendered["overview"])
        for key, spec in self.us.services.items():
            if not spec.get("network_mode"):
                self.assertEqual(self.rendered["overview"].count(architecture.ident("overview", key) + '["'), 1)

    def test_network_and_overview_service_placement(self):
        def placements(graph):
            group, result = None, {}
            for line in graph.splitlines():
                if line.startswith("    subgraph "):
                    group = line.split()[1].split("[")[0]
                elif line == "    end":
                    group = None
                elif '["' in line:
                    result[line.strip().split("[")[0]] = group
            return result

        for package in self.packages:
            actual = placements(self.rendered[f"network-{package.short}"])
            for key, service in package.services.items():
                if service.get("network_mode"):
                    continue
                networks = service["networks"]
                expected = (architecture.ident(package.short, "network", next(iter(networks)))
                            if len(networks) == 1 else None)
                self.assertEqual(actual[architecture.ident(package.short, "service", key)], expected)
        actual = placements(self.rendered["overview"])
        for key, service in self.us.services.items():
            if key == "firewall" or service.get("network_mode"):
                continue
            network = ("platform" if key in self.us.platform["source"]["services"]
                       else architecture.primary_network(self.us, key))
            self.assertEqual(actual[architecture.ident("overview", key)],
                             architecture.ident("overview", "zone", network))

    def test_route_summary_limits_prefixes_and_counts_all_hidden_locations(self):
        paths = {"/apps/a/", "/apps/b/", "/apps/c/", "/apps/d/",
                 "= /api/project", "~ ^/api/(device|settings)$"}
        self.assertEqual(architecture.route_summary(paths),
                         "/apps/a/\n/apps/b/\n/apps/c/\n+3 more")

    def test_nginx_api_exceptions_and_app_upstreams(self):
        routes = architecture.routes(ROOT, self.us)
        fuxa = routes[("proxy", "fuxa_hmi:1881")]
        for path in ("version", "settings", "project", "projectData", "device", "alarmsHistory", "resources"):
            self.assertIn("/api/" + path, fuxa)
        self.assertIn("/apps/openplc/", routes[("proxy", "openplc:8080")])
        self.assertIn("/containd/", routes[("proxy", "firewall:8080")])
        self.assertNotIn("/apps/firewall", self.rendered["request-flow"])
        self.assertNotIn(("proxy", "fuxa_hmi:1881"), architecture.routes(ROOT, self.eu))
        self.assertNotIn("^/", self.rendered["request-flow"])
        self.assertNotIn("/api/projectData", self.rendered["request-flow"])
        self.assertIn("+", self.rendered["request-flow"])
        self.assertIn("fuxa_hmi:1881 (US)", self.rendered["request-flow"])
        self.assertIn("openplc_eu:8080 (EU)", self.rendered["request-flow"])

    def test_terminal_counts_and_request_nodes_are_connected(self):
        import re
        graph = self.rendered["request-flow"]
        for package in self.packages:
            count = sum(bool(s.get("node")) for s in package.nodes.values())
            self.assertIn(f"{package.short.upper()} {count}", graph)
        self.assertNotIn("capbank-1", graph)
        self.assertNotIn("subgraph", graph)
        nodes = re.findall(r'^    (\w+)\["', graph, re.M)
        self.assertEqual(len(nodes), len(set(nodes)))
        self.assertLessEqual(len(nodes), 30)
        for node in nodes:
            self.assertTrue(any(node in line and ('-->' in line or '-.->' in line)
                                for line in graph.splitlines()), node)
        self.assertEqual(graph.count('127.0.0.1:8088:8080'), 1)
        self.assertEqual(graph.count('127.0.0.1:9080:8080'), 1)
        for line in graph.splitlines():
            if 'nginx proxy' not in line and architecture.ident("request", "proxy") + ' -->' in line:
                text = line.split('|"')[1].split('"|')[0]
                self.assertLessEqual(sum(p.startswith('/') for p in text.split('<br/>')), 3)

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
            path.write_text(path.read_text().replace("<!-- BEGIN GENERATED: overview -->", "<!-- BEGIN GENERATED: overview -->\n%% stale", 1))
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
