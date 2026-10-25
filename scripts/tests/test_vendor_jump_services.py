import subprocess
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]


class VendorPortalSiteTests(unittest.TestCase):
    def test_portal_install_preserves_webtop_site_and_is_idempotent(self):
        script = (ROOT / "scripts/vendor-jump-services.sh").read_text()
        install = script.split(
            "cat > /etc/nginx/sites-available/vendor-portal", 1
        )[1].split('if ! pgrep -x nginx', 1)[0]
        install = "cat > /etc/nginx/sites-available/vendor-portal" + install
        with tempfile.TemporaryDirectory() as directory:
            nginx = Path(directory).resolve()
            available = nginx / "sites-available"
            enabled = nginx / "sites-enabled"
            available.mkdir()
            enabled.mkdir()
            webtop = "server { listen 3000; }\nserver { listen 3001 ssl; }\n"
            (available / "default").write_text(webtop)
            default = enabled / "default"
            default.symlink_to(available / "default")
            for _ in range(2):
                subprocess.run(
                    ["bash", "-e", "-c", install.replace("/etc/nginx", directory)],
                    check=True,
                )
                self.assertEqual(default.resolve(), available / "default")
                self.assertEqual(default.read_text(), webtop)
                portal = enabled / "vendor-portal"
                self.assertEqual(portal.resolve(), available / "vendor-portal")
                self.assertIn("listen 80 default_server;", portal.read_text())
                self.assertIn("listen 443 ssl default_server;", portal.read_text())

    def test_nginx_validation_failure_is_not_hidden_by_a_pipeline(self):
        script = (ROOT / "scripts/vendor-jump-services.sh").read_text()
        startup = script.split('if ! pgrep -x nginx', 1)[1].split("\nfi", 1)[0]
        self.assertIn("\n    nginx -t\n", startup)


if __name__ == "__main__":
    unittest.main()
