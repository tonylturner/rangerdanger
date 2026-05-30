// scenario-description.ts — pure-logic parser for lab step
// description bodies. Extracted from scenario-runner.tsx so it can
// be unit-tested without dragging React in.
//
// The grammar (informally): a description is a sequence of segments.
// Most lines are prose. Indented lines starting with a recognized
// tool name are command blocks. Triple-colon fences (:::hint,
// :::decision, :::findings-panel) open structured segments that
// extend until the next ::: line.
//
// See docs/lab-authoring.md for the author-facing reference.

const CMD_TOOL_RE = /^(nmap|mbpoll|dnp3poll|dnp3cmd|curl|tshark|tcpdump|nc|telnet|ssh|wget|ls|grep|cat|docker)\s/;

// :::hint Title  ...content...  :::
// Opens a collapsible hint panel in the rendered description. Anything
// between the opening and closing fence is captured as the hint body.
const HINT_OPEN_RE = /^:::hint(?:\s+(.+))?$/;
const HINT_CLOSE_RE = /^:::$/;

// :::decision id=enterprise-to-field [options=BLOCK,RESTRICT,ALLOW,LOG]
// Question prose (markdown allowed)
// :::
// Renders an inline question + dropdown. The student's answer is
// persisted to localStorage (key derived from scenario id + decision
// id) so refreshes don't lose it AND so subsequent labs can read
// the same selections later.
const DECISION_OPEN_RE = /^:::decision\s+(.+)$/;

// :::findings-panel from=<scenarioId> [title="..."]
// id1: label1
// id2: label2
// :::
// Renders a read-only panel showing the student's recorded answers
// for a list of upstream decision ids. Each line in the body is
// "<decisionId>: <human label>"; the panel reads each id's
// localStorage value and shows it.
const FINDINGS_PANEL_OPEN_RE = /^:::findings-panel(?:\s+(.+))?$/;

// :::plan-coverage [title="..."]
// :::
// Renders the same per-requirement coverage view that Lab 1.4's
// DecisionPanel uses, but inline at any point in a description.
// Reads the saved remediation plan from localStorage and computes
// coverage against the 1.3 design verdicts. Used in Lab 2.4 to
// surface "what did your plan close vs defer" without making the
// student manually recall and re-list it.
const PLAN_COVERAGE_OPEN_RE = /^:::plan-coverage(?:\s+(.+))?$/;

// :::track-picker
// :::
// Renders the Guided / Technical fork picker for the firewall labs.
// Body ignored. Writes the choice to localStorage via
// useFirewallTrack so later steps and labs can read it.
const TRACK_PICKER_OPEN_RE = /^:::track-picker(?:\s+(.+))?$/;

// :::guided
//   prose only shown on the guided track
// :::
// :::technical
//   prose only shown on the technical track
// :::
// These fence content that varies between firewall-track choices.
// When the track is null (not yet picked), both blocks render so
// students see both perspectives. The parser captures the body and
// the renderer in scenario-runner.tsx filters at render time.
const GUIDED_OPEN_RE = /^:::guided$/;
const TECHNICAL_OPEN_RE = /^:::technical$/;

// :::generate-traffic-button
// :::
// Renders the "Generate Traffic" button inline at the YAML position
// where the directive sits. Without this, the legacy text-trigger
// fallback in scenario-runner.tsx renders the button at the bottom
// of the step body; the directive lets a lab author drop the button
// at the natural reading point — i.e. exactly where the lab text
// says to start traffic generation. Body ignored.
const GENERATE_TRAFFIC_BTN_RE = /^:::generate-traffic-button(?:\s+(.+))?$/;

// Default options match the workshop's risk-verdict vocabulary. The
// "BLOCK and LOG" combo is its own entry because that's how the
// answer key for unauthorized-writes is stated (block-plus-log is a
// different operational decision than block-without-log).
export const DECISION_DEFAULT_OPTIONS = ["BLOCK", "BLOCK and LOG", "RESTRICT", "ALLOW"];

export type FindingsPanelItem = { id: string; label: string };

export type Segment =
  | { type: "prose"; value: string }
  | { type: "cmd"; value: string }
  | { type: "hint"; title: string; value: string }
  | {
      type: "decision";
      id: string;
      options: string[];
      body: string;
      defaultFrom: string;
      correct: string;
    }
  | {
      type: "findingsPanel";
      sourceScenario: string;
      title: string;
      items: FindingsPanelItem[];
    }
  | {
      type: "planCoverage";
      title: string;
    }
  | {
      type: "trackPicker";
    }
  | {
      type: "generateTrafficButton";
    }
  | {
      type: "trackOnly";
      track: "guided" | "technical";
      // Body is parsed recursively into segments by the renderer so
      // nested commands / hints / decisions still work inside a
      // track-conditional block.
      body: string;
    };

// Parse `id=foo options=A,B,C default-from=lab:dec correct=X` style
// attributes on the decision fence. Quotes around values are
// tolerated for embedded spaces. Unknown attributes are ignored.
//
// default-from=<scenario.id>:<decisionId> lets a downstream lab
// inherit the student's earlier verdict as the starting value of a
// new decision. The student can confirm or change it; the new
// decision still lives under its own storage key.
//
// correct=<value> is the answer-key attribute. Set it on
// observation/factual decisions where there's a known-correct
// answer; leave it OFF on judgment-call decisions.
export function parseDecisionAttrs(attrs: string): {
  id: string;
  options: string[];
  defaultFrom: string;
  correct: string;
} {
  const idMatch = attrs.match(/\bid=("([^"]+)"|(\S+))/);
  const optMatch = attrs.match(/\boptions=("([^"]+)"|(\S+))/);
  const fromMatch = attrs.match(/\bdefault-from=("([^"]+)"|(\S+))/);
  const correctMatch = attrs.match(/\bcorrect=("([^"]+)"|(\S+))/);
  const id = (idMatch?.[2] ?? idMatch?.[3] ?? "").trim();
  const optsRaw = (optMatch?.[2] ?? optMatch?.[3] ?? "").trim();
  const defaultFrom = (fromMatch?.[2] ?? fromMatch?.[3] ?? "").trim();
  const correct = (correctMatch?.[2] ?? correctMatch?.[3] ?? "").trim();
  const options = optsRaw
    ? optsRaw.split(",").map((o) => o.trim()).filter(Boolean)
    : DECISION_DEFAULT_OPTIONS;
  return { id, options, defaultFrom, correct };
}

// Parse `from=foo title="..."` attributes on the findings-panel fence.
export function parseFindingsAttrs(attrs: string): {
  sourceScenario: string;
  title: string;
} {
  const fromMatch = attrs.match(/\bfrom=("([^"]+)"|(\S+))/);
  const titleMatch = attrs.match(/\btitle=("([^"]+)"|(\S+))/);
  const sourceScenario = (fromMatch?.[2] ?? fromMatch?.[3] ?? "").trim();
  const title = (titleMatch?.[2] ?? titleMatch?.[3] ?? "Inherited findings").trim();
  return { sourceScenario, title };
}

// Parse `title="..."` attributes on the plan-coverage fence.
export function parsePlanCoverageAttrs(attrs: string): { title: string } {
  const titleMatch = attrs.match(/\btitle=("([^"]+)"|(\S+))/);
  const title = (titleMatch?.[2] ?? titleMatch?.[3] ?? "Your plan coverage").trim();
  return { title };
}

/**
 * Split a description body into interleaved prose, command, hint,
 * decision, and findingsPanel segments. Order in the output preserves
 * the order in the input.
 */
export function splitDescription(text: string): Segment[] {
  const result: Segment[] = [];
  const lines = text.split("\n");
  let prose: string[] = [];
  let i = 0;

  const flushProse = () => {
    // Drop pure-whitespace prose buffers — blank lines between
    // fences (which is the natural way to write a YAML
    // description) shouldn't produce empty <p> segments. The UI
    // renderer was filtering these out anyway; cleaner to do it
    // here so test output is also clean.
    if (prose.some((l) => l.trim() !== "")) {
      result.push({ type: "prose", value: prose.join("\n") });
    }
    prose = [];
  };

  while (i < lines.length) {
    const trimmed = lines[i].trim();
    const hintOpen = HINT_OPEN_RE.exec(trimmed);
    if (hintOpen) {
      flushProse();
      const title = hintOpen[1]?.trim() || "Reveal answer";
      const body: string[] = [];
      i++;
      while (i < lines.length && !HINT_CLOSE_RE.test(lines[i].trim())) {
        body.push(lines[i]);
        i++;
      }
      if (i < lines.length) i++; // skip closing fence
      result.push({ type: "hint", title, value: body.join("\n") });
      continue;
    }
    const planCoverageOpen = PLAN_COVERAGE_OPEN_RE.exec(trimmed);
    if (planCoverageOpen) {
      flushProse();
      const { title } = parsePlanCoverageAttrs(planCoverageOpen[1] ?? "");
      // Eat through the closing :::, body is ignored.
      i++;
      while (i < lines.length && !HINT_CLOSE_RE.test(lines[i].trim())) {
        i++;
      }
      if (i < lines.length) i++;
      result.push({ type: "planCoverage", title });
      continue;
    }
    if (TRACK_PICKER_OPEN_RE.test(trimmed)) {
      flushProse();
      // Eat through closing :::, body ignored.
      i++;
      while (i < lines.length && !HINT_CLOSE_RE.test(lines[i].trim())) {
        i++;
      }
      if (i < lines.length) i++;
      result.push({ type: "trackPicker" });
      continue;
    }
    if (GENERATE_TRAFFIC_BTN_RE.test(trimmed)) {
      flushProse();
      // Eat through closing :::, body ignored.
      i++;
      while (i < lines.length && !HINT_CLOSE_RE.test(lines[i].trim())) {
        i++;
      }
      if (i < lines.length) i++;
      result.push({ type: "generateTrafficButton" });
      continue;
    }
    if (GUIDED_OPEN_RE.test(trimmed) || TECHNICAL_OPEN_RE.test(trimmed)) {
      flushProse();
      const track = GUIDED_OPEN_RE.test(trimmed) ? "guided" : "technical";
      const body: string[] = [];
      i++;
      while (i < lines.length && !HINT_CLOSE_RE.test(lines[i].trim())) {
        body.push(lines[i]);
        i++;
      }
      if (i < lines.length) i++;
      result.push({ type: "trackOnly", track, body: body.join("\n") });
      continue;
    }
    const findingsOpen = FINDINGS_PANEL_OPEN_RE.exec(trimmed);
    if (findingsOpen) {
      flushProse();
      const { sourceScenario, title } = parseFindingsAttrs(findingsOpen[1] ?? "");
      const items: FindingsPanelItem[] = [];
      i++;
      while (i < lines.length && !HINT_CLOSE_RE.test(lines[i].trim())) {
        const line = lines[i].trim();
        if (line) {
          const m = line.match(/^([A-Za-z0-9._-]+)\s*:\s*(.+)$/);
          if (m) items.push({ id: m[1], label: m[2] });
        }
        i++;
      }
      if (i < lines.length) i++;
      if (sourceScenario && items.length > 0) {
        result.push({ type: "findingsPanel", sourceScenario, title, items });
      }
      continue;
    }
    const decisionOpen = DECISION_OPEN_RE.exec(trimmed);
    if (decisionOpen) {
      flushProse();
      const { id, options, defaultFrom, correct } = parseDecisionAttrs(decisionOpen[1]);
      const body: string[] = [];
      i++;
      while (i < lines.length && !HINT_CLOSE_RE.test(lines[i].trim())) {
        body.push(lines[i]);
        i++;
      }
      if (i < lines.length) i++;
      // Skip silently if id is missing — author error. Better to
      // render the question as prose than to swallow it.
      if (id) {
        result.push({
          type: "decision",
          id,
          options,
          body: body.join("\n"),
          defaultFrom,
          correct,
        });
      } else {
        prose.push(...body);
      }
      continue;
    }
    // A line is a command block only if it is indented (has leading
    // whitespace) AND starts with a recognized tool prefix. The
    // indentation distinguishes a code snippet from a prose sentence
    // that happens to begin with a tool name (e.g. "tshark uses
    // display filters").
    const isIndented = /^\s+\S/.test(lines[i]);
    if (isIndented && CMD_TOOL_RE.test(trimmed)) {
      flushProse();
      let cmd = trimmed;
      while (cmd.endsWith("\\") && i + 1 < lines.length) {
        i++;
        cmd += "\n  " + lines[i].trim();
      }
      result.push({ type: "cmd", value: cmd });
    } else {
      prose.push(lines[i]);
    }
    i++;
  }
  flushProse();
  return result;
}
