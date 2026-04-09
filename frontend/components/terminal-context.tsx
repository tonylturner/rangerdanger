"use client";

import React, { createContext, useContext, useState, useCallback, ReactNode } from "react";
import { NodeTerminal } from "./node-terminal";

// ── Context ──────────────────────────────────────────────────────

type TerminalContextValue = {
  /** Set of node IDs with alive terminal sessions */
  openTerminals: Set<string>;
  /** Open a terminal (idempotent) */
  open: (nodeId: string) => void;
  /** Close a terminal */
  close: (nodeId: string) => void;
};

const Ctx = createContext<TerminalContextValue | null>(null);

export function useTerminals() {
  const ctx = useContext(Ctx);
  if (!ctx) throw new Error("useTerminals requires TerminalProvider");
  return ctx;
}

// ── Provider ─────────────────────────────────────────────────────
// Rendered in the app layout. Tracks which terminals have been
// opened so pages can share that knowledge.

export function TerminalProvider({ children }: { children: ReactNode }) {
  const [openTerminals, setOpen] = useState<Set<string>>(new Set());

  const open = useCallback((nodeId: string) => {
    setOpen((prev) => {
      if (prev.has(nodeId)) return prev;
      return new Set([...prev, nodeId]);
    });
  }, []);

  const close = useCallback((nodeId: string) => {
    setOpen((prev) => {
      const next = new Set(prev);
      next.delete(nodeId);
      return next;
    });
  }, []);

  return (
    <Ctx.Provider value={{ openTerminals, open, close }}>
      {children}
    </Ctx.Provider>
  );
}

// ── SharedTerminalPanel ──────────────────────────────────────────
// Drop-in panel that renders terminals using the hidden-mount
// pattern (display:none/block). Used by exercise runner and
// network console. The shared context tracks which terminals
// have been opened across pages.

export function SharedTerminalPanel({
  nodes,
  activeNode,
  height = 300,
}: {
  nodes: string[];
  activeNode: string;
  onSelectNode?: (nodeId: string) => void;
  height?: number;
}) {
  const { openTerminals, open } = useTerminals();

  // Ensure all listed nodes are tracked as open
  React.useEffect(() => {
    nodes.forEach((n) => open(n));
  }, [nodes, open]);

  return (
    <div className="relative" style={{ height }}>
      {nodes.filter((n) => openTerminals.has(n)).map((nodeId) => {
        const isVisible = activeNode === nodeId;
        return (
          <div
            key={nodeId}
            className="absolute inset-0"
            style={{ display: isVisible ? "block" : "none" }}
          >
            <NodeTerminal nodeId={nodeId} labId="workshop" hideHeader />
          </div>
        );
      })}
    </div>
  );
}
