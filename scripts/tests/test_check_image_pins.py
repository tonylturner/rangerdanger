"""Exercise the image-pin gate using isolated tracked Git fixtures."""
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
SCRIPT = ROOT / "scripts/check-image-pins.sh"
IMAGE = "alpine:3.22@sha256:" + "a" * 64


class ImagePinTests(unittest.TestCase):
    def check(self, files, expected=0, untracked=None):
        with tempfile.TemporaryDirectory(prefix="image-pins-") as directory:
            root = Path(directory)
            subprocess.run(["git", "init", "-q", directory], check=True, timeout=10)
            for name, content in files.items():
                path = root / name
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text(content)
            subprocess.run(["git", "-C", directory, "add", "--", *files],
                           check=True, timeout=10)
            for name, content in (untracked or {}).items():
                (root / name).write_text(content)
            result = subprocess.run(["bash", str(SCRIPT), directory],
                                    capture_output=True, text=True, timeout=10)
            self.assertEqual(result.returncode, expected, result.stdout + result.stderr)
            return result

    def test_pinned_base_and_internal_stages(self):
        self.check({"Dockerfile": f"FROM {IMAGE} AS base\nFROM base AS final\n"})

    def test_all_dockerfile_naming_conventions_and_spaces(self):
        self.check({name: f"FROM {IMAGE}\n" for name in
                    ["Dockerfile.api", "nested/Dockerfile", "a b/tools.Dockerfile"]})

    def test_dockerignore_files_are_not_dockerfiles(self):
        self.check({"Dockerfile": f"FROM {IMAGE}\n",
                    "Dockerfile.api.dockerignore": "FROM floating\n"})

    def test_floating_from_fails_with_location(self):
        result = self.check({"Dockerfile": "# heading\nFROM alpine:3.22\n"}, 1)
        self.assertIn("Dockerfile:2:", result.stderr)

    def test_invalid_digest_and_missing_tag(self):
        for image in ["alpine:3.22@sha256:" + "a" * 63,
                      "alpine:3.22@sha256:" + "g" * 64,
                      "alpine@sha256:" + "a" * 64,
                      "registry:5000/alpine@sha256:" + "a" * 64,
                      "alpine:3.22@sha256:" + "A" * 64,
                      "alpine", "scratch"]:
            with self.subTest(image=image):
                self.check({"Dockerfile": f"FROM {image}\n"}, 1)

    def test_registry_port_and_platform(self):
        self.check({"Dockerfile": f"FROM --platform=$BUILDPLATFORM registry:5000/{IMAGE}\n"})

    def test_global_arg_expansion_and_quotes(self):
        self.check({"Dockerfile": f'ARG BASE="{IMAGE}"\nARG BASE_IMAGE=$BASE\n'
                    "FROM ${BASE_IMAGE} AS build\nFROM build\n"})

    def test_unused_image_arg_default_is_checked(self):
        self.check({"Dockerfile": f"ARG BASE_IMAGE=alpine:3.22\nFROM {IMAGE}\n"}, 1)

    def test_floating_arg_used_as_base(self):
        self.check({"Dockerfile": "ARG BASE=alpine:3.22\nFROM ${BASE}\n"}, 1)

    def test_undefined_cyclic_and_complex_args_fail_closed(self):
        for prefix, expression in [("", "${BASE}"), ("ARG BASE=$BASE\n", "$BASE"),
                                   ("", "${BASE:-alpine:3.22}")]:
            with self.subTest(expression=expression):
                self.check({"Dockerfile": prefix + f"FROM {expression}\n"}, 1)

    def test_stage_local_arg_does_not_change_global_base(self):
        self.check({"Dockerfile": f"ARG BASE={IMAGE}\nFROM $BASE\n"
                    "ARG BASE=alpine:3.22\nFROM $BASE\n"})
        self.check({"Dockerfile": f"FROM {IMAGE}\nARG BASE={IMAGE}\nFROM $BASE\n"}, 1)

    def test_non_image_build_args_are_exempt(self):
        self.check({"Dockerfile": f"FROM {IMAGE}\nARG VERSION=dev\nARG DATE\n"})

    def test_comments_case_and_continuations(self):
        self.check({"Dockerfile": "# FROM floating\nARG BASE_IMAGE=\\\n"
                    f"  {IMAGE}\nfRoM \\\n# comment between continuation lines\n"
                    " $BASE_IMAGE aS build\nFROM BUILD\n"})

    def test_backtick_escape(self):
        self.check({"Dockerfile": f"# escape=`\nFROM `\n{IMAGE}\n"})

    def test_heredoc_body_is_not_a_docker_instruction(self):
        self.check({"Dockerfile": f"FROM {IMAGE}\nRUN <<'EOF'\n"
                    "FROM alpine:3.22\nEOF\nCOPY <<-FILE /tmp/a\n"
                    "\tFROM alpine:3.22\n\tFILE\n"})

    def test_unterminated_continuation_and_heredoc_fail(self):
        for suffix in ["FROM \\\n", "RUN <<EOF\nFROM floating\n"]:
            with self.subTest(suffix=suffix):
                self.check({"Dockerfile": f"FROM {IMAGE}\n{suffix}"}, 1)

    def test_malformed_from_fails(self):
        for value in ["alpine:3.22 extra", "", f"{IMAGE} AS"]:
            with self.subTest(value=value):
                self.check({"Dockerfile": f"FROM {value}\n"}, 1)
        self.check({"Dockerfile": "FROM\n"}, 1)

    def test_python_harness_peers(self):
        peers = {
            "Dockerfile": f"FROM {IMAGE}\n",
            "services/iec104/check/run.sh": f"PY={IMAGE}\n",
            "services/iec104/check/reference/build.py": f'PEER_IMAGE = "{IMAGE}"\n',
        }
        self.check(peers)
        for name in list(peers)[1:]:
            with self.subTest(name=name):
                self.check({**peers, name: peers[name].replace(IMAGE, "python:3.12-alpine")}, 1)
                self.check({**peers, name: "# removed reference\n"}, 1)

    def test_empty_repository_fails(self):
        self.check({"README.md": "No Dockerfiles\n"}, 1)

    def test_untracked_dockerfiles_are_not_release_inputs(self):
        self.check({"Dockerfile": f"FROM {IMAGE}\n"},
                   untracked={"Dockerfile.scratch": "FROM floating\n"})

    def test_ci_runs_gate_and_regression_tests(self):
        workflow = (ROOT / ".github/workflows/ci.yml").read_text()
        lint = workflow.split("\n  eu-gates:")[0]
        self.assertIn("bash scripts/check-image-pins.sh", lint)
        self.assertIn("python3 -m unittest scripts.tests.test_check_image_pins", lint)

    def test_repository_passes(self):
        result = subprocess.run(["bash", str(SCRIPT), str(ROOT)],
                                capture_output=True, text=True, timeout=30)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)


if __name__ == "__main__":
    unittest.main()
