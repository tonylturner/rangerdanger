import type { ReactNode } from "react";
import { CATCH_UP_POLICY_HELP, CATCH_UP_TITLE } from "../lib/catch-up";

export function CatchUp({ children }: { children: ReactNode }) {
  return (
    <details className="rounded-lg border border-sky-800/60 bg-sky-950/20 p-3 text-xs text-slate-300">
      <summary className="cursor-pointer font-semibold text-sky-300 focus-visible:outline-2 focus-visible:outline-sky-400">
        {CATCH_UP_TITLE}
      </summary>
      <p className="my-2">{CATCH_UP_POLICY_HELP}</p>
      {children}
    </details>
  );
}
