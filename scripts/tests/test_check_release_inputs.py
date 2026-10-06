from __future__ import annotations

import json
import sys
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "scripts"))

import check_release_inputs


def inventory_inputs() -> dict[str, set[tuple[str, str]]]:
    """Reviewed envelope oracle transcribed from ledger §2.2."""
    common = {
        ("file", "services/go.mod"),
        ("file", "dnp3go/go.mod"),
        ("glob", "services/shared/*.go"),
        ("file", "scripts/set-gateway.sh"),
        ("file", "scripts/rtac-harden.sh"),
        ("file", "scripts/rtac-route-monitor.sh"),
    }
    dnp3_sim = common | {("glob", "dnp3go/*.go")}
    return {
        "rangerdanger-backend": {("dir", "backend")},
        "rangerdanger-frontend": {("dir", "frontend")},
        "rangerdanger-kali": {("dir", "dnp3go")},
        "rangerdanger-vendor-jump": {
            ("dir", "dnp3go"),
            ("file", "scripts/vendor-jump-services.sh"),
            ("file", "scripts/vendor-jump-x11vnc-service.sh"),
        },
        "rangerdanger-eng-ws": {("dir", "dnp3go")},
        "rangerdanger-corp-ws": set(),
        "rangerdanger-openplc": {
            ("dir", "dnp3go"),
            ("file", "scripts/openplc-entrypoint.sh"),
            ("file", "scripts/set-gateway.sh"),
        },
        "rangerdanger-fuxa-hmi": {
            ("file", "scripts/set-gateway.sh"),
            ("file", "scripts/fuxa-entrypoint.sh"),
        },
        "rangerdanger-rtac-sim": dnp3_sim | {
            ("glob", "services/rtac-sim/*.go"),
            ("glob", "dnp3go/cmd/dnp3poll/*.go"),
            ("glob", "dnp3go/cmd/dnp3cmd/*.go"),
            ("file", "scripts/rtac-mgmt-init.sh"),
        },
        "rangerdanger-relay-sim": dnp3_sim | {("glob", "services/relay-sim/*.go")},
        "rangerdanger-recloser-sim": dnp3_sim | {
            ("glob", "services/recloser-sim/*.go")},
        "rangerdanger-regulator-sim": dnp3_sim | {
            ("glob", "services/regulator-sim/*.go")},
        "rangerdanger-capbank-sim": dnp3_sim | {
            ("glob", "services/capbank-sim/*.go")},
        "rangerdanger-historian-sim": common | {
            ("glob", "services/historian-sim/*.go")},
        "rangerdanger-gps-sim": common | {("glob", "services/gps-sim/*.go")},
        "rangerdanger-opendss-sim": {
            ("file", "services/opendss-sim/requirements.txt"),
            ("file", "services/opendss-sim/main.py"),
            ("file", "services/opendss-sim/circuit.py"),
            ("file", "services/opendss-sim/models.py"),
            ("file", "services/opendss-sim/runtime_state.py"),
            ("file", "services/opendss-sim/time_utils.py"),
            ("dir", "services/opendss-sim/dss"),
            ("file", "scripts/set-gateway.sh"),
        },
    }


class CheckReleaseInputsTests(unittest.TestCase):
    def setUp(self) -> None:
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name)
        (self.root / ".github").mkdir()
        (self.root / "scripts").mkdir()
        (self.root / "services").mkdir()
        (self.root / "docker-compose.release.yml").write_text(
            "services:\n  app:\n    image: ghcr.io/tonylturner/rangerdanger-other:latest\n",
            encoding="utf-8",
        )
        self.image = {
            "image": "rangerdanger-test",
            "dockerfile": "Dockerfile.test",
            "target": "",
            "platforms": ["linux/amd64"],
            "always_build": False,
            "inputs": [{"kind": "file", "path": "src/input.txt"}],
        }
        (self.root / "Dockerfile.test").write_text(
            "FROM alpine\nCOPY src/input.txt /input.txt\n",
            encoding="utf-8",
        )
        (self.root / "src").mkdir()
        (self.root / "src/input.txt").write_text("input\n", encoding="utf-8")
        self._save_inventory([self.image])

    def tearDown(self) -> None:
        self.temp.cleanup()

    def _save_inventory(self, images: list[dict[str, object]]) -> None:
        (self.root / ".github/release-images.json").write_text(
            json.dumps({
                "schema": 1,
                "policy_version": 1,
                "registry": "ghcr.io",
                "namespace": "tonylturner",
                "images": images,
            }, indent=2),
            encoding="utf-8",
        )

    def _messages(self) -> list[str]:
        return [item.message for item in check_release_inputs.validate(
            self.root, Path(".github/release-images.json"))]

    def test_inventory_envelopes_match_the_per_image_ledger(self) -> None:
        inventory_path = ROOT / ".github/release-images.json"
        inventory = json.loads(inventory_path.read_text(encoding="utf-8"))
        expected = inventory_inputs()
        actual = {
            item["image"]: {(entry["kind"], entry["path"]) for entry in item["inputs"]}
            for item in inventory["images"]
        }
        self.assertEqual(set(actual), set(expected))
        self.assertEqual(actual, expected)

    def test_catches_copy_source_not_covered_by_reviewed_envelope(self) -> None:
        with (self.root / "src/extra.txt").open("w", encoding="utf-8") as output:
            output.write("extra\n")
        dockerfile = self.root / "Dockerfile.test"
        dockerfile.write_text(
            dockerfile.read_text(encoding="utf-8") + "COPY src/extra.txt /extra.txt\n",
            encoding="utf-8",
        )
        messages = self._messages()
        self.assertTrue(any("uncovered local COPY/ADD source 'src/extra.txt'" in
                            message for message in messages))

    def test_allowlist_rejects_each_unbounded_recipe_construct(self) -> None:
        cases = {
            "RUN --mount=type=bind,target=/tmp,source=. true": "type=bind",
            "RUN --mount=from=other,source=/tmp,target=/tmp true": "from=",
            "ADD https://example.invalid/archive.tar /data/": "URL or Git reference",
            "ADD src/archive.tar /data/": "archive source",
            "ONBUILD RUN echo inherited": "ONBUILD",
            "COPY --from=external-context /file /file": "named contexts",
            "COPY --from=0 /file /file": "numeric stage references",
            "COPY $SOURCE /file": "variable interpolation",
            "COPY src/**/*.txt /data/": "outside the file/dir/single-segment glob model",
            "ARG BUILD_FLAG\nRUN echo $BUILD_FLAG": "consumes ARG(s) BUILD_FLAG",
            "ARG BUILD_FLAG\nRUN echo ${BUILD_FLAG:-default}":
                "consumes ARG(s) BUILD_FLAG",
        }
        for recipe, expected in cases.items():
            with self.subTest(recipe=recipe):
                self.image["inputs"] = []
                self._save_inventory([self.image])
                (self.root / "Dockerfile.test").write_text(
                    f"FROM alpine\n{recipe}\n", encoding="utf-8")
                messages = self._messages()
                self.assertTrue(
                    any(expected in message for message in messages),
                    f"missing {expected!r} in {messages!r}",
                )

    def test_always_build_is_the_alternate_for_arg_consumption(self) -> None:
        self.image["always_build"] = True
        self._save_inventory([self.image])
        (self.root / "Dockerfile.test").write_text(
            "FROM alpine\nARG VERSION\nRUN echo $VERSION\n",
            encoding="utf-8",
        )
        self.assertFalse(any("consumes ARG" in message for message in self._messages()))

    def test_context_root_copy_requires_explicit_recursive_root_envelope(self) -> None:
        (self.root / "Dockerfile.test").write_text(
            "FROM alpine\nCOPY . /app/\n", encoding="utf-8")
        messages = self._messages()
        self.assertTrue(any("uncovered local COPY/ADD source '.'" in message
                            for message in messages))

        self.image["inputs"] = [{"kind": "dir", "path": "."}]
        self._save_inventory([self.image])
        messages = self._messages()
        self.assertFalse(any("uncovered local COPY/ADD source '.'" in message
                             for message in messages))

    def test_single_segment_glob_does_not_cover_nested_paths(self) -> None:
        self.image["inputs"] = [{"kind": "glob", "path": "src/*.txt"}]
        self._save_inventory([self.image])
        nested = self.root / "src/nested"
        nested.mkdir()
        (nested / "extra.txt").write_text("nested\n", encoding="utf-8")
        (self.root / "Dockerfile.test").write_text(
            "FROM alpine\nCOPY src/nested/extra.txt /extra.txt\n",
            encoding="utf-8",
        )
        messages = self._messages()
        self.assertTrue(any("uncovered local COPY/ADD source" in message
                            for message in messages))

    def test_workflow_declared_build_context_is_rejected(self) -> None:
        workflow = self.root / ".github/workflows/release.yml"
        workflow.parent.mkdir(parents=True, exist_ok=True)
        workflow.write_text("build-contexts: extra=./external\n", encoding="utf-8")
        messages = self._messages()
        self.assertTrue(any("contexts are not bounded" in message
                            for message in messages))

    def test_inventory_compose_equality_and_target_resolution(self) -> None:
        self.image["target"] = "missing-stage"
        self._save_inventory([self.image])
        messages = self._messages()
        self.assertTrue(any("does not resolve to a Dockerfile stage" in
                            message for message in messages))
        self.assertTrue(any("first-party Compose image is missing" in
                            message for message in messages))
        self.assertTrue(any("inventory image is not a first-party image" in
                            message for message in messages))


if __name__ == "__main__":
    unittest.main()
