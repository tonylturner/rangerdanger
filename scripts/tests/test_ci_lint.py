"""Keep workflow and tracked-shell lint as hard CI gates."""
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
        self.assertIn("releases/download/v0.11.0/shellcheck-v0.11.0.linux.x86_64.tar.xz", commands)
        self.assertIn("8c3be12b05d5c177a04c29e3c78ce89ac86f1595681cab149b65b97c4e227198", commands)
        self.assertIn("sha256sum --check --strict", commands)
        self.assertIn("actionlint .github/workflows/*.yml", commands)
        self.assertIn("git ls-files -z -- '*.sh' | xargs -0 shellcheck -x -S warning", commands)
        for step in job["steps"]:
            self.assertFalse(step.get("continue-on-error", False))
