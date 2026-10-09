"use client";

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ReactNode, useState } from "react";
import { isRangeNotReady } from "../lib/range";
import { RangeProvider } from "./range-context";
import { TooltipProvider } from "./ui/tooltip";

// A range that is not ready answers the same until the transition
// ends, and RangeProvider refetches everything when it does; retrying
// would only add load while containers start.
function retry(failureCount: number, error: Error): boolean {
  return !isRangeNotReady(error) && failureCount < 3;
}

export function ClientProviders({ children }: { children: ReactNode }) {
  const [queryClient] = useState(() => new QueryClient({ defaultOptions: { queries: { retry } } }));
  return (
    <QueryClientProvider client={queryClient}>
      <TooltipProvider delayDuration={150} skipDelayDuration={0}>
        <RangeProvider>{children}</RangeProvider>
      </TooltipProvider>
    </QueryClientProvider>
  );
}
