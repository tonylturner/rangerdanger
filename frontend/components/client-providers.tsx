"use client";

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ReactNode, useState } from "react";
import { TerminalProvider } from "./terminal-context";
import { TooltipProvider } from "./ui/tooltip";

export function ClientProviders({ children }: { children: ReactNode }) {
  const [queryClient] = useState(() => new QueryClient());
  return (
    <QueryClientProvider client={queryClient}>
      <TooltipProvider delayDuration={150} skipDelayDuration={0}>
        <TerminalProvider>{children}</TerminalProvider>
      </TooltipProvider>
    </QueryClientProvider>
  );
}
