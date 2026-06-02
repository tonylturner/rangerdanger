"use client";

import React from "react";
import Markdown from "marked-react";
import Link from "next/link";

// Dark-themed markdown renderer for exercise step descriptions and hint
// bodies. Styled to match the slate theme used throughout the app.
//
// Custom overrides:
//   - Links: internal paths (starting with /) route through Next.js Link
//     so the exercise runner stays a SPA. External links open in a new tab.
//   - Code spans (single backticks) get amber mono styling to match our
//     command block aesthetic. They do NOT get Run/Copy buttons - that's
//     reserved for block-level tool commands detected by splitDescription
//     upstream.
//   - Tables get overflow-x auto so wide tables don't break layout.
//   - Blockquotes get a cyan left border and tinted background, usable
//     as lightweight admonitions ("> Note: ...").

const renderer = {
  heading(children: React.ReactNode, level: 1 | 2 | 3 | 4 | 5 | 6) {
    const sizes: Record<number, string> = {
      1: "text-base font-bold text-slate-100 mt-3 mb-2",
      2: "text-sm font-bold text-slate-100 mt-3 mb-2",
      3: "text-xs font-bold uppercase tracking-wider text-slate-400 mt-3 mb-1",
      4: "text-[11px] font-bold uppercase tracking-wider text-slate-500 mt-2 mb-1",
      5: "text-[11px] font-semibold text-slate-400 mt-2 mb-1",
      6: "text-[10px] font-semibold text-slate-500 mt-2 mb-1",
    };
    const cls = sizes[level] || sizes[3];
    const props = { key: `h-${Math.random()}`, className: cls };
    switch (level) {
      case 1: return <h1 {...props}>{children}</h1>;
      case 2: return <h2 {...props}>{children}</h2>;
      case 3: return <h3 {...props}>{children}</h3>;
      case 4: return <h4 {...props}>{children}</h4>;
      case 5: return <h5 {...props}>{children}</h5>;
      default: return <h6 {...props}>{children}</h6>;
    }
  },

  paragraph(children: React.ReactNode) {
    return (
      <p key={`p-${Math.random()}`} className="text-sm text-slate-300 leading-relaxed">
        {children}
      </p>
    );
  },

  link(href: string, text: React.ReactNode) {
    const isInternal = href.startsWith("/") && !href.startsWith("//");
    if (isInternal) {
      return (
        <Link
          key={`lk-${Math.random()}`}
          href={href}
          className="text-cyan-400 hover:text-cyan-300 underline underline-offset-2"
        >
          {text}
        </Link>
      );
    }
    return (
      <a
        key={`lk-${Math.random()}`}
        href={href}
        target="_blank"
        rel="noopener noreferrer"
        className="text-cyan-400 hover:text-cyan-300 underline underline-offset-2"
      >
        {text}
      </a>
    );
  },

  codespan(code: React.ReactNode) {
    return (
      <code
        key={`cs-${Math.random()}`}
        className="rounded bg-slate-800 px-1.5 py-0.5 font-mono text-[11px] text-amber-300"
      >
        {code}
      </code>
    );
  },

  code(code: React.ReactNode) {
    // Fenced code blocks from the YAML (triple backticks). These are NOT
    // the same as our Run/Copy command blocks - those are detected in
    // splitDescription upstream and rendered before this renderer sees
    // anything. Fenced code here is for multi-line snippets authors want
    // to display without a Run button (e.g., sample output).
    return (
      <pre
        key={`code-${Math.random()}`}
        className="my-2 overflow-x-auto rounded border border-slate-700 bg-slate-950 px-3 py-2 font-mono text-[11px] text-amber-300"
      >
        <code>{code}</code>
      </pre>
    );
  },

  blockquote(children: React.ReactNode) {
    return (
      <blockquote
        key={`bq-${Math.random()}`}
        className="my-2 rounded-r border-l-2 border-cyan-700 bg-cyan-950/20 px-3 py-2 text-sm text-slate-300"
      >
        {children}
      </blockquote>
    );
  },

  list(children: React.ReactNode, ordered: boolean, start: number | undefined) {
    const cls = "my-2 space-y-1 text-sm text-slate-300 ml-5";
    if (ordered) {
      return (
        <ol key={`ol-${Math.random()}`} className={`${cls} list-decimal`} start={start}>
          {children}
        </ol>
      );
    }
    return (
      <ul key={`ul-${Math.random()}`} className={`${cls} list-disc`}>
        {children}
      </ul>
    );
  },

  listItem(children: React.ReactNode[]) {
    return (
      <li key={`li-${Math.random()}`} className="leading-relaxed">
        {children}
      </li>
    );
  },

  checkbox(checked: React.ReactNode) {
    return (
      <input
        key={`cb-${Math.random()}`}
        type="checkbox"
        checked={!!checked}
        readOnly
        className="mr-1 accent-cyan-500"
      />
    );
  },

  strong(children: React.ReactNode) {
    return (
      <strong key={`b-${Math.random()}`} className="font-bold text-slate-100">
        {children}
      </strong>
    );
  },

  em(children: React.ReactNode) {
    return (
      <em key={`i-${Math.random()}`} className="italic text-slate-200">
        {children}
      </em>
    );
  },

  del(children: React.ReactNode) {
    return (
      <del key={`d-${Math.random()}`} className="text-slate-500 line-through">
        {children}
      </del>
    );
  },

  table(children: React.ReactNode[]) {
    return (
      <div key={`tbl-${Math.random()}`} className="my-3 overflow-x-auto">
        <table className="w-full border-collapse text-xs">
          {children}
        </table>
      </div>
    );
  },

  tableHeader(children: React.ReactNode) {
    return (
      <thead key={`th-${Math.random()}`} className="bg-slate-900/80">
        {children}
      </thead>
    );
  },

  tableBody(children: React.ReactNode[]) {
    return <tbody key={`tb-${Math.random()}`}>{children}</tbody>;
  },

  tableRow(children: React.ReactNode[]) {
    return (
      <tr key={`tr-${Math.random()}`} className="border-b border-slate-800">
        {children}
      </tr>
    );
  },

  tableCell(children: React.ReactNode[], flags: { header?: boolean; align?: string | null }) {
    const align = flags.align ? `text-${flags.align}` : "text-left";
    if (flags.header) {
      return (
        <th
          key={`td-${Math.random()}`}
          className={`px-2 py-1.5 font-bold text-slate-300 ${align}`}
        >
          {children}
        </th>
      );
    }
    return (
      <td
        key={`td-${Math.random()}`}
        className={`px-2 py-1.5 text-slate-300 ${align}`}
      >
        {children}
      </td>
    );
  },

  hr() {
    return <hr key={`hr-${Math.random()}`} className="my-3 border-slate-800" />;
  },
};

type MarkdownProseProps = {
  children: string;
};

export function MarkdownProse({ children }: MarkdownProseProps) {
  if (!children || !children.trim()) return null;
  return <Markdown value={children} renderer={renderer} gfm breaks={false} />;
}
