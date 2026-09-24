export function stripMarkdownForExcerpt(md: string): string {
  // Strip code fences, headings, link syntax, emphasis markers, bullet markers,
  // and HTML for a clean snippet - first paragraph only.
  const noFences = md.replace(/```[\s\S]*?```/g, "");
  const firstPara = noFences.split(/\n\n+/).find((p) => p.trim().length > 0) || "";
  return firstPara
    .replace(/^#{1,6}\s+/gm, "")
    .replace(/[*_`>~]/g, "")
    .replace(/\[([^\]]+)\]\([^)]+\)/g, "$1")
    .replace(/\s+/g, " ")
    .trim();
}

export function readingMinutes(md: string): number {
  const words = md.replace(/```[\s\S]*?```/g, "").split(/\s+/).filter(Boolean).length;
  return Math.max(1, Math.round(words / 220));
}

export function buildSnippet(body: string, q: string): string {
  const lower = body.toLowerCase();
  const i = lower.indexOf(q);
  if (i < 0) return stripMarkdownForExcerpt(body).slice(0, 180);
  const start = Math.max(0, i - 60);
  const end = Math.min(body.length, i + q.length + 120);
  const slice = body.slice(start, end).replace(/\s+/g, " ").trim();
  return (start > 0 ? "… " : "") + slice + (end < body.length ? " …" : "");
}
