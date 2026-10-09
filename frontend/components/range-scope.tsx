"use client";

import type { ReactNode } from "react";
import { RangeBanner } from "./range-banner";
import { useRange } from "./range-context";
import { TerminalProvider } from "./terminal-context";

// The page area. Everything inside belongs to one range: when the
// range changes, the key change remounts it, so component state and
// terminal sessions from the old range are dropped along with the
// cached queries.
export function RangeScope({ children }: { children: ReactNode }) {
  const { scopeKey } = useRange();
  return (
    <section className="flex-1">
      <RangeBanner />
      <TerminalProvider key={scopeKey}>{children}</TerminalProvider>
    </section>
  );
}
