"use client";

import { useState } from "react";

export function CopyButton({ text, label = "Copy", quiet = false }: { text: string; label?: string; quiet?: boolean }) {
  const [feedback, setFeedback] = useState("");
  const copy = async () => {
    try {
      if (!navigator.clipboard) throw new Error("Clipboard unavailable");
      await navigator.clipboard.writeText(text);
      setFeedback("Copied");
    } catch {
      setFeedback("Copy failed — select and copy the text manually");
    }
  };
  return (
    <span className="inline-flex items-center gap-2">
      <button
        onClick={copy}
        className={`rounded-sm px-1 py-0.5 text-[11px] text-slate-300 hover:text-white focus-visible:outline-2 focus-visible:outline-sky-400 ${quiet ? "sm:opacity-0 group-hover:opacity-100 group-focus-within:opacity-100 focus:opacity-100" : ""}`}
      >
        {label}
      </button>
      <span role="status" className="text-[11px] text-slate-300">{feedback}</span>
    </span>
  );
}
