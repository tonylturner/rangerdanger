"use client";

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ReactNode, useState } from "react";
import { retryQuery } from "../lib/query-retry";
import { RangeProvider } from "./range-context";
import { TooltipProvider } from "./ui/tooltip";

export function ClientProviders({ children }: { children: ReactNode }) {
  const [queryClient] = useState(() => new QueryClient({ defaultOptions: { queries: { retry: retryQuery } } }));
  return (
    <QueryClientProvider client={queryClient}>
      <TooltipProvider delayDuration={150} skipDelayDuration={0}>
        <RangeProvider>{children}</RangeProvider>
      </TooltipProvider>
    </QueryClientProvider>
  );
}
