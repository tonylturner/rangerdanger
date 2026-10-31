#!/usr/bin/env bash
# Static preparation-order checks: never source or execute a gate.
set -euo pipefail

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
python3 - "$repo" <<'PY'
from pathlib import Path
import re
import shlex
import sys
import unittest

REPO = Path(sys.argv.pop())


def dead_applies(source):
    """Scan top-level gate bodies using the modules' function layout."""
    failures = []
    for gate, body in re.findall(
        r"^(eu_gate_G\d+)\(\) \{(.*?)^\}", source, re.M | re.S
    ):
        # Preserve command boundaries, but not quoted jq bodies or comments.
        lexer = shlex.shlex(body.replace("\\\n", " "), posix=False,
                            punctuation_chars=";&|\n")
        lexer.whitespace = " \t\r"
        lexer.whitespace_split = True
        # Keep quotes so a quoted "#" is not a comment. Handle comments here
        # because shlex otherwise swallows their terminating command boundary.
        lexer.commenters = ""
        commands = [[]]
        comment = False
        for token in lexer:
            if token.startswith("#"):
                comment = True
            if token and all(char in ";&|\n" for char in token):
                if not comment or "\n" in token:
                    commands.append([])
                if "\n" in token:
                    comment = False
            elif not comment:
                commands[-1].append(token)
        pending = False
        for command in commands:
            while command and command[0] in ("if", "elif", "then", "else", "!", "do"):
                command.pop(0)
            if not command:
                continue
            name = command[0]
            if name == "eu_reset_all":
                if pending:
                    failures.append(gate)
                pending = False
            elif name == "eu_apply_named":
                pending = True
            elif name not in ("return", "eu_set_profile"):
                # An assertion or other substantive operation consumes the
                # policy. G23 legitimately probes Improved before resetting.
                pending = False
    return failures


def gate(body, number=99):
    return f"eu_gate_G{number}() {{\n{body}\n}}\n"


class PolicyResetOrder(unittest.TestCase):
    def test_all_gate_modules(self):
        modules = sorted((REPO / "scripts/lib").glob("eu-gates-*.sh"))
        self.assertTrue(modules)
        for module in modules:
            with self.subTest(module=module.name):
                self.assertEqual(dead_applies(module.read_text()), [])

    def test_dead_preparation(self):
        self.assertEqual(dead_applies(gate(
            '  eu_apply_named improved || return # policy setup; not evidence\n'
            '  # Reset overwrites the policy.\n'
            '  eu_reset_all || return'
        )), ["eu_gate_G99"])

    def test_old_g29_order(self):
        self.assertEqual(dead_applies(gate(
            '  if eu_apply_named improved && eu_set_profile false && eu_reset_all \\\n'
            '    && eu_lab_reconnect_cc; then\n'
            '    pass "ready"\n'
            '  fi', 29
        )), ["eu_gate_G29"])

    def test_reset_before_apply(self):
        self.assertEqual(dead_applies(gate(
            '  if eu_reset_all && eu_apply_named improved \\\n'
            '    && eu_lab_reconnect_cc; then\n'
            '    pass "ready"\n'
            '  fi'
        )), [])

    def test_assertions_before_reset_are_not_dead(self):
        self.assertEqual(dead_applies(gate(
            '  eu_apply_named improved || return\n'
            '  eu_assert_jq "$API_BODY" "true" "policy evidence"\n'
            '  eu_reset_all || return'
        )), [])

    def test_function_boundaries_and_quoted_commands(self):
        self.assertEqual(dead_applies(
            gate('  eu_apply_named improved || return', 98)
            + gate('  # eu_apply_named improved\n'
                   '  pass "eu_apply_named improved && eu_reset_all"\n'
                   '  eu_reset_all || return')
        ), [])

    def test_g11_custom_policy_follows_reset(self):
        source = (REPO / "scripts/lib/eu-gates-resilience.sh").read_text()
        body = re.search(r"^eu_gate_G11\(\) \{(.*?)^\}", source, re.M | re.S)[1]
        self.assertEqual(body.count("eu_reset_all"), 1)
        self.assertLess(body.index("eu_reset_all"), body.index("eu_commit_manual_policy"))


unittest.main()
PY
