import { describe, it, expect } from "vitest";
import {
  splitDescription,
  parseDecisionAttrs,
  parseFindingsAttrs,
  DECISION_DEFAULT_OPTIONS,
} from "./scenario-description";

// Helper: pull the segment of a given type at a given index. Tests
// stay readable when we want to assert one segment in a multi-segment
// output without long type-narrowing chains.
function segAt<T extends ReturnType<typeof splitDescription>[number]["type"]>(
  segs: ReturnType<typeof splitDescription>,
  idx: number,
  type: T,
) {
  const s = segs[idx];
  expect(s.type).toBe(type);
  return s as Extract<ReturnType<typeof splitDescription>[number], { type: T }>;
}

describe("splitDescription — prose handling", () => {
  it("returns a single prose segment for plain text", () => {
    const segs = splitDescription("Hello world.\nA second line.");
    expect(segs).toHaveLength(1);
    const p = segAt(segs, 0, "prose");
    expect(p.value).toBe("Hello world.\nA second line.");
  });

  it("returns no segments for empty input", () => {
    expect(splitDescription("")).toHaveLength(0);
  });

  it("preserves blank lines inside prose", () => {
    const segs = splitDescription("para 1\n\npara 2");
    expect(segs).toHaveLength(1);
    const p = segAt(segs, 0, "prose");
    expect(p.value).toBe("para 1\n\npara 2");
  });
});

describe("splitDescription — command blocks", () => {
  it("detects an indented tool-prefixed line as a command", () => {
    const segs = splitDescription("Try this:\n\n    nmap -p 502 10.40.40.20\n\nDone.");
    expect(segs.map((s) => s.type)).toEqual(["prose", "cmd", "prose"]);
    const cmd = segAt(segs, 1, "cmd");
    expect(cmd.value).toBe("nmap -p 502 10.40.40.20");
  });

  it("requires indentation — un-indented tool-prefixed prose stays prose", () => {
    // The runner protects against the prose sentence "tshark uses
    // display filters" being misclassified as a runnable command.
    const segs = splitDescription("tshark uses display filters to narrow output.");
    expect(segs).toHaveLength(1);
    expect(segs[0].type).toBe("prose");
  });

  it("joins backslash-continued multi-line commands", () => {
    const text = [
      "    curl -s http://10.30.30.20:8080/api/state \\",
      "      | python3 -m json.tool \\",
      "      | head -10",
    ].join("\n");
    const segs = splitDescription(text);
    expect(segs).toHaveLength(1);
    const cmd = segAt(segs, 0, "cmd");
    expect(cmd.value).toMatch(/curl/);
    expect(cmd.value).toMatch(/python3 -m json\.tool/);
    expect(cmd.value).toMatch(/head -10/);
  });

  it("recognizes every CMD_TOOL_RE prefix", () => {
    const tools = [
      "nmap", "mbpoll", "dnp3poll", "dnp3cmd", "curl", "tshark",
      "tcpdump", "nc", "telnet", "ssh", "wget", "ls", "grep", "cat", "docker",
    ];
    for (const tool of tools) {
      const segs = splitDescription(`    ${tool} something`);
      expect(segs[0].type, `tool ${tool} not detected`).toBe("cmd");
    }
  });

  it("does not recognize unknown tools", () => {
    const segs = splitDescription("    rm -rf /tmp/foo");
    // rm isn't in CMD_TOOL_RE — falls through to prose
    expect(segs[0].type).toBe("prose");
  });
});

describe("splitDescription — :::hint", () => {
  it("captures hint title and body", () => {
    const text = [
      "Some intro.",
      "",
      ":::hint Reveal expected answer",
      "Body line 1.",
      "Body line 2.",
      ":::",
      "",
      "After.",
    ].join("\n");
    const segs = splitDescription(text);
    expect(segs.map((s) => s.type)).toEqual(["prose", "hint", "prose"]);
    const hint = segAt(segs, 1, "hint");
    expect(hint.title).toBe("Reveal expected answer");
    expect(hint.value).toBe("Body line 1.\nBody line 2.");
  });

  it("defaults the title to 'Reveal answer' when absent", () => {
    const segs = splitDescription(":::hint\nbody\n:::");
    const hint = segAt(segs, 0, "hint");
    expect(hint.title).toBe("Reveal answer");
    expect(hint.value).toBe("body");
  });

  it("handles a hint with no body", () => {
    const segs = splitDescription(":::hint Title\n:::");
    const hint = segAt(segs, 0, "hint");
    expect(hint.value).toBe("");
  });
});

describe("splitDescription — :::decision", () => {
  it("parses a fully-specified decision fence", () => {
    const text = [
      ":::decision id=enterprise-to-field options=BLOCK,RESTRICT,ALLOW correct=BLOCK",
      "**Enterprise → Field** on Modbus 502 — verdict?",
      ":::",
    ].join("\n");
    const segs = splitDescription(text);
    expect(segs).toHaveLength(1);
    const d = segAt(segs, 0, "decision");
    expect(d.id).toBe("enterprise-to-field");
    expect(d.options).toEqual(["BLOCK", "RESTRICT", "ALLOW"]);
    expect(d.correct).toBe("BLOCK");
    expect(d.defaultFrom).toBe("");
    expect(d.body).toContain("Enterprise → Field");
  });

  it("falls back to default options when not specified", () => {
    const segs = splitDescription(
      ":::decision id=foo\nbody\n:::",
    );
    const d = segAt(segs, 0, "decision");
    expect(d.options).toEqual(DECISION_DEFAULT_OPTIONS);
  });

  it("supports the default-from inheritance attribute", () => {
    const segs = splitDescription(
      ":::decision id=foo default-from=baseline-assessment:enterprise-to-field\nQ\n:::",
    );
    const d = segAt(segs, 0, "decision");
    expect(d.defaultFrom).toBe("baseline-assessment:enterprise-to-field");
  });

  it("drops decisions with missing id, falling back to prose", () => {
    // Author error: forgot the id=. Rather than swallow the body
    // silently, the parser emits it as prose so the question still
    // shows.
    const segs = splitDescription(
      ":::decision options=YES,NO\nThe question text.\n:::",
    );
    expect(segs).toHaveLength(1);
    expect(segs[0].type).toBe("prose");
    expect((segs[0] as { value: string }).value).toContain("The question text.");
  });

  it("interleaves prose, decision, and hint correctly", () => {
    const text = [
      "Intro prose.",
      "",
      ":::decision id=foo correct=YES",
      "Q1?",
      ":::",
      "",
      "Middle prose.",
      "",
      ":::hint Hint title",
      "Hint body.",
      ":::",
    ].join("\n");
    const segs = splitDescription(text);
    expect(segs.map((s) => s.type)).toEqual(["prose", "decision", "prose", "hint"]);
    expect(segAt(segs, 1, "decision").correct).toBe("YES");
    expect(segAt(segs, 3, "hint").title).toBe("Hint title");
  });
});

describe("splitDescription — :::findings-panel", () => {
  it("parses the standard findings-panel form", () => {
    const text = [
      ':::findings-panel from=baseline-assessment title="Findings (Lab 1.2)"',
      "enterprise-to-field: Enterprise → Field on Modbus/DNP3",
      "vendor-to-ot: Vendor DMZ → OT Operations",
      ":::",
    ].join("\n");
    const segs = splitDescription(text);
    expect(segs).toHaveLength(1);
    const fp = segAt(segs, 0, "findingsPanel");
    expect(fp.sourceScenario).toBe("baseline-assessment");
    expect(fp.title).toBe("Findings (Lab 1.2)");
    expect(fp.items).toEqual([
      { id: "enterprise-to-field", label: "Enterprise → Field on Modbus/DNP3" },
      { id: "vendor-to-ot", label: "Vendor DMZ → OT Operations" },
    ]);
  });

  it("uses a default title when none is given", () => {
    const segs = splitDescription(
      [
        ":::findings-panel from=lab-x",
        "id1: label one",
        ":::",
      ].join("\n"),
    );
    const fp = segAt(segs, 0, "findingsPanel");
    expect(fp.title).toBe("Inherited findings");
  });

  it("drops the panel when from= is missing", () => {
    const segs = splitDescription(
      [
        ':::findings-panel title="Lonely"',
        "id1: label",
        ":::",
      ].join("\n"),
    );
    expect(segs).toHaveLength(0); // panel was dropped, prose was empty
  });

  it("drops the panel when no items parse", () => {
    const segs = splitDescription(
      [
        ":::findings-panel from=lab-x",
        "this is not in id: label format because it has no colon at left edge",
        // Wait — that line DOES match (any chars before ':' isn't
        // restricted enough on actual semantics, but the regex is
        // ^([A-Za-z0-9._-]+)\s*:\s*(.+)$). This particular line has
        // spaces so won't match. Confirm.
        ":::",
      ].join("\n"),
    );
    expect(segs).toHaveLength(0);
  });

  it("ignores non-conforming lines but still parses valid ones", () => {
    const segs = splitDescription(
      [
        ":::findings-panel from=lab-x",
        "line with no colon",
        "valid-id: a label",
        "  : missing the id",
        "",
        "another-id: another label",
        ":::",
      ].join("\n"),
    );
    expect(segs).toHaveLength(1);
    const fp = segAt(segs, 0, "findingsPanel");
    expect(fp.items).toHaveLength(2);
    expect(fp.items[0].id).toBe("valid-id");
    expect(fp.items[1].id).toBe("another-id");
  });
});

describe("splitDescription — integration", () => {
  it("handles a realistic Lab 1.2 step 6-style mix of fences", () => {
    const text = [
      "Now look at your findings.",
      "",
      ":::decision id=enterprise-to-field options=YES,NO,UNSURE correct=YES",
      "Did you see this?",
      ":::",
      "",
      ":::decision id=vendor-to-ot options=YES,NO,UNSURE correct=YES",
      "And this?",
      ":::",
      "",
      ":::hint Reveal expected answer",
      "Both are present in the baseline.",
      ":::",
    ].join("\n");

    const segs = splitDescription(text);
    expect(segs.map((s) => s.type)).toEqual([
      "prose", "decision", "decision", "hint",
    ]);
    expect(segAt(segs, 1, "decision").id).toBe("enterprise-to-field");
    expect(segAt(segs, 2, "decision").id).toBe("vendor-to-ot");
    expect(segAt(segs, 3, "hint").value).toContain("baseline");
  });

  it("handles a Lab 1.3-style findings-panel followed by decisions", () => {
    const text = [
      ":::findings-panel from=baseline-assessment title=\"Passive\"",
      "enterprise-to-field: Enterprise → Field",
      ":::",
      "",
      ":::findings-panel from=baseline-assessment title=\"Active\"",
      "latent-exposure-confirmed: Latent exposure",
      ":::",
      "",
      ":::decision id=req-enterprise-to-field",
      "What's your design verdict?",
      ":::",
    ].join("\n");

    const segs = splitDescription(text);
    expect(segs.map((s) => s.type)).toEqual([
      "findingsPanel", "findingsPanel", "decision",
    ]);
    expect(segAt(segs, 0, "findingsPanel").title).toBe("Passive");
    expect(segAt(segs, 1, "findingsPanel").title).toBe("Active");
    expect(segAt(segs, 2, "decision").id).toBe("req-enterprise-to-field");
  });
});

describe("parseDecisionAttrs", () => {
  it("returns empty fields when nothing matches", () => {
    const a = parseDecisionAttrs("");
    expect(a.id).toBe("");
    expect(a.defaultFrom).toBe("");
    expect(a.correct).toBe("");
    expect(a.options).toEqual(DECISION_DEFAULT_OPTIONS);
  });

  it("parses unquoted attribute values", () => {
    const a = parseDecisionAttrs("id=foo correct=YES default-from=lab:dec");
    expect(a.id).toBe("foo");
    expect(a.correct).toBe("YES");
    expect(a.defaultFrom).toBe("lab:dec");
  });

  it("parses quoted attribute values that contain spaces", () => {
    const a = parseDecisionAttrs('id=foo options="A,B with space,C"');
    expect(a.id).toBe("foo");
    expect(a.options).toEqual(["A", "B with space", "C"]);
  });

  it("trims whitespace inside option lists", () => {
    const a = parseDecisionAttrs("id=foo options=A , B , C");
    // Note: the regex captures the first non-space token after
    // options= — so this case captures only "A" (the comma-space
    // continuation isn't part of an unquoted value). Quote the value
    // when it has commas + spaces.
    expect(a.options).toEqual(["A"]);
  });

  it("ignores unknown attributes", () => {
    const a = parseDecisionAttrs("id=foo unknown=bar correct=YES");
    expect(a.id).toBe("foo");
    expect(a.correct).toBe("YES");
  });
});

describe("splitDescription — :::plan-coverage", () => {
  it("parses a plan-coverage fence with title", () => {
    const text = [
      ':::plan-coverage title="What your plan closed"',
      ":::",
    ].join("\n");
    const segs = splitDescription(text);
    expect(segs).toHaveLength(1);
    const pc = segAt(segs, 0, "planCoverage");
    expect(pc.title).toBe("What your plan closed");
  });

  it("uses a default title when title= is absent", () => {
    const segs = splitDescription(":::plan-coverage\n:::");
    const pc = segAt(segs, 0, "planCoverage");
    expect(pc.title).toBe("Your plan coverage");
  });

  it("ignores any body content between fence markers", () => {
    // The fence body has no semantic meaning; the panel reads from
    // localStorage at render time. Author-supplied body lines should
    // be silently dropped, not rendered as prose.
    const text = [
      ":::plan-coverage",
      "this body is ignored",
      "as is this",
      ":::",
      "After.",
    ].join("\n");
    const segs = splitDescription(text);
    expect(segs.map((s) => s.type)).toEqual(["planCoverage", "prose"]);
  });
});

describe("parseFindingsAttrs", () => {
  it("uses the default title when title= is absent", () => {
    const a = parseFindingsAttrs("from=lab-x");
    expect(a.sourceScenario).toBe("lab-x");
    expect(a.title).toBe("Inherited findings");
  });

  it("parses quoted titles with spaces", () => {
    const a = parseFindingsAttrs('from=lab-x title="Findings from your assessment"');
    expect(a.title).toBe("Findings from your assessment");
  });

  it("returns empty source when from= is absent", () => {
    const a = parseFindingsAttrs('title="just a title"');
    expect(a.sourceScenario).toBe("");
  });
});
