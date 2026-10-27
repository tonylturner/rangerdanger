"""Offline delivery boundary checks. Never invoke setup or real Docker."""
from __future__ import annotations

import hashlib
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "scripts"))
import check_release_inputs


class DeliveryNetworks(unittest.TestCase):
    def test_release_hashes_cover_network_only_changes(self) -> None:
        original = check_release_inputs.release_compose_metadata(ROOT, "v9.9.9")
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            models = [original["platform"], *original["packages"].values()]
            for model in models:
                source = ROOT / model["file"]
                target = root / model["file"]
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_bytes(source.read_bytes() + b"\n# network revision\n")
            revised = check_release_inputs.release_compose_metadata(root, "v9.9.9")
            changed = [revised["platform"], *revised["packages"].values()]
            for before, after in zip(models, changed):
                with self.subTest(file=before["file"]):
                    self.assertEqual(before["services"], after["services"])
                    self.assertNotEqual(before["sha256"], after["sha256"])
                    self.assertEqual(
                        after["sha256"],
                        hashlib.sha256((root / after["file"]).read_bytes()).hexdigest(),
                    )

    def test_nginx_rejects_socket_peer_before_all_locations(self) -> None:
        config = (ROOT / "proxy/nginx.conf").read_text()
        self.assertIn("geo $desktop_source {", config)
        self.assertIn("10.98.0.0/16 1;", config)
        deny = config.index("if ($desktop_source) { return 403; }")
        self.assertLess(deny, config.index("location /"))
        self.assertLess(deny, config.index("include /etc/nginx/rd-routes/"))
        self.assertNotIn("real_ip_header", config)
        self.assertNotIn("set_real_ip_from", config)

    def test_ownership_preflight(self) -> None:
        script = (ROOT / "setup.sh").read_text()
        # Only extract the ownership loop: no installer or live CLI is run.
        loop = script[script.index("for DELIVERY_NET in "):script.index('MGMT_OWNER=""')]
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            docker = root / "docker"
            docker.write_text(
                '#!/bin/bash\n'
                'if [ "$1 $2" = "network ls" ]; then\n'
                '    [ "$FAIL_LS" != yes ] || exit 8\n'
                '    [ "$EXISTS" != yes ] || printf "network-id\\n"\n'
                '    exit 0\n'
                'fi\n'
                'if [ "$1 $2" = "network inspect" ] && [ "$3" = "-f" ]; then\n'
                '    printf "%s" "$OWNER"\n'
                '    exit 0\n'
                'fi\n'
                'exit 99\n'
            )
            docker.chmod(0o755)
            command = (
                'set -eu\nPLATFORM_PROJECT=rangerdanger-platform\n'
                # Exercise the ownership loop independently of its tested
                # timeout helper; this fixture checks only network outcomes.
                'docker_bounded() { local limit="$1"; shift; docker "$@"; }\n'
                'die() { printf "%s\\n" "$*" >&2; exit 1; }\n' + loop
            )
            for owner, exists, passed in [
                ("", "no", True),
                ("rangerdanger-platform", "yes", True),
                ("rangerdanger", "yes", False),
                ("foreign", "yes", False),
                ("", "yes", False),
            ]:
                with self.subTest(owner=owner, exists=exists):
                    env = dict(os.environ, PATH=f"{root}:/usr/bin:/bin",
                               OWNER=owner, EXISTS=exists, FAIL_LS="no")
                    result = subprocess.run(
                        ["bash", "-c", command],
                        env=env, capture_output=True, text=True, check=False,
                    )
                    self.assertEqual(result.returncode, 0 if passed else 1, result.stderr)
                    if not passed:
                        self.assertIn("No network was removed.", result.stderr)

            result = subprocess.run(
                ["bash", "-c", command],
                env=dict(os.environ, PATH=f"{root}:/usr/bin:/bin",
                         OWNER="", EXISTS="no", FAIL_LS="yes"),
                capture_output=True, text=True, check=False,
            )
            self.assertEqual(result.returncode, 1)
            self.assertIn("Could not query network rangerdanger_desk_corp", result.stderr)


if __name__ == "__main__":
    unittest.main()
