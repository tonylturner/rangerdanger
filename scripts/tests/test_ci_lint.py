"""Keep workflow, tracked-shell and unused Go code lint as hard CI gates."""
from pathlib import Path
import unittest

import yaml

ROOT = Path(__file__).resolve().parents[2]


class CILintTests(unittest.TestCase):
    def test_lint_job_checks_workflows_and_all_tracked_shell_scripts(self):
        workflow = yaml.safe_load((ROOT / ".github/workflows/ci.yml").read_text())
        job = workflow["jobs"]["lint"]
        self.assertFalse(job.get("continue-on-error", False))
        self.assertEqual(job["env"]["SHELLCHECK_OPTS"], "-x")
        commands = "\n".join(step.get("run", "") for step in job["steps"])
        self.assertIn("go install github.com/rhysd/actionlint/cmd/actionlint@v1.7.12", commands)
        self.assertNotIn("apt-get install -y shellcheck", commands)
        self.assertIn(
            "https://github.com/koalaman/shellcheck/releases/download/v0.11.0/"
            "shellcheck-v0.11.0.linux.x86_64.tar.xz",
            commands,
        )
        self.assertIn(
            "8c3be12b05d5c177a04c29e3c78ce89ac86f1595681cab149b65b97c4e227198",
            commands,
        )
        self.assertIn("sha256sum --check --strict", commands)
        self.assertIn(
            'sudo install -m 0755 "$tmp/shellcheck-v0.11.0/shellcheck" /usr/local/bin/shellcheck',
            commands,
        )
        self.assertIn("actionlint .github/workflows/*.yml", commands)
        config = yaml.safe_load((ROOT / ".github/actionlint.yaml").read_text())
        self.assertEqual(
            config["self-hosted-runner"]["labels"], ["ubuntu-26.04", "ubuntu-26.04-arm"]
        )
        self.assertIn("git ls-files -z -- '*.sh' | xargs -0 shellcheck -x -S warning", commands)
        for step in job["steps"]:
            self.assertFalse(step.get("continue-on-error", False))

    def test_all_go_modules_enforce_pinned_unused_code_check(self):
        workflow = yaml.safe_load((ROOT / ".github/workflows/ci.yml").read_text())
        for module in ("backend", "services", "dnp3go", "iec104go"):
            with self.subTest(module=module):
                job = workflow["jobs"][module]
                self.assertFalse(job.get("continue-on-error", False))
                self.assertNotIn("if", job)
                steps = [
                    step for step in job["steps"]
                    if step.get("name") == "Unused Go code (U1000)"
                ]
                self.assertEqual(len(steps), 1)
                step = steps[0]
                self.assertEqual(step["working-directory"], module)
                self.assertEqual(
                    step["run"],
                    "go run honnef.co/go/tools/cmd/staticcheck@2026.2.2 -checks U1000 ./...",
                )
                self.assertFalse(step.get("continue-on-error", False))
                self.assertNotIn("if", step)
