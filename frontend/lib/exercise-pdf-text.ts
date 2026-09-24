// ── Text sanitization ──────────────────────────────────────────────
//
// Roboto has full Latin Extended + symbol coverage,
// so we don't need to strip Unicode anymore. We only normalize a few
// chars that look bad in print or trip layout: non-breaking space to
// regular space, smart quotes to straight (matches the rest of the
// PDF's typography). Em-dashes, arrows, bullets, multiplication signs
// etc. render fine in Roboto and are kept as-is.
export function sanitizeText(s: string): string {
  return s
    .replace(/[\u2018\u2019\u201A\u2032]/g, "'")
    .replace(/[\u201C\u201D\u201E\u2033]/g, '"')
    .replace(/\u00A0/g, " ")
    // Arrows aren't in the Roboto-latin subset we ship — render them as
    // ASCII so they don't fall back to a notdef glyph that looks like
    // an apostrophe.
    .replace(/\u2192/g, "->")
    .replace(/\u2190/g, "<-")
    .replace(/\u2194/g, "<->")
    .replace(/\u21D2/g, "=>")
    .replace(/\u21D0/g, "<=")
    .replace(/\u2191/g, "^")
    .replace(/\u2193/g, "v");
}

// Long unbreakable runs in command/code blocks (URLs, JSON payloads,
// quote-bound filter strings) can't be word-wrapped by react-pdf because
// they have no whitespace breakpoints. Hard-wrap long lines at natural
// punctuation by inserting a real newline + continuation indent. We
// can't use \u200B (zero-width space) here because that codepoint is
// outside the Roboto-latin font subset and would render as notdef.
export function softWrapCode(s: string, maxLen = 78): string {
  const wrap = (line: string): string => {
    if (line.length <= maxLen) return line;
    // Find a breakpoint near maxLen at a sensible character.
    const breakChars = /[\s,=]/;
    let cut = -1;
    for (let i = Math.min(maxLen, line.length - 1); i > maxLen / 2; i--) {
      if (breakChars.test(line[i])) {
        cut = i + 1;
        break;
      }
    }
    if (cut < 0) cut = maxLen;
    const head = line.slice(0, cut).replace(/\s+$/, "");
    const tail = line.slice(cut).replace(/^\s+/, "");
    return head + "\n  " + wrap(tail);
  };
  return s.split("\n").map(wrap).join("\n");
}

// ── Segment splitter (same logic as scenario-runner) ────────────────

const CMD_TOOL_RE =
  /^(nmap|mbpoll|dnp3poll|dnp3cmd|curl|tshark|tcpdump|nc|telnet|ssh|wget|ls|grep|cat|docker)\s/;
const HINT_OPEN_RE = /^:::hint(?:\s+(.+))?$/;
const HINT_CLOSE_RE = /^:::$/;

export type Segment =
  | { type: "prose"; value: string }
  | { type: "cmd"; value: string }
  | { type: "hint"; title: string; value: string };

export function splitDescription(text: string): Segment[] {
  const result: Segment[] = [];
  const lines = text.split("\n");
  let prose: string[] = [];
  let i = 0;

  const flushProse = () => {
    if (prose.length > 0) {
      result.push({ type: "prose", value: prose.join("\n") });
      prose = [];
    }
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
      if (i < lines.length) i++;
      result.push({ type: "hint", title, value: body.join("\n") });
      continue;
    }
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
