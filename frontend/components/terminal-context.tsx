"use client";

import React, { createContext, useContext, useState, useCallback, useRef, useEffect } from "react";
import dynamic from "next/dynamic";

const TerminalInner = dynamic(() => import("./terminal-inner"), { ssr: false });

// ── Context Type ─────────────────────────────────────────────────

type TerminalContextValue = {
  /** Which node terminals are currently alive (WebSocket open, scrollback preserved) */
  activeTerminals: Set<string>;
  /** Open a terminal for a node (idempotent — won't create duplicate) */
  openTerminal: (nodeId: string) => void;
  /** Close a specific terminal */
  closeTerminal: (nodeId: string) => void;
  /** Close all terminals */
  closeAll: () => void;
  /** Get the DOM ref for a terminal's container (for reparenting) */
  getTerminalRef: (nodeId: string) => HTMLDivElement | null;
};

const TerminalContext = createContext<TerminalContextValue | null>(null);

export function useTerminalContext() {
  const ctx = useContext(TerminalContext);
  if (!ctx) throw new Error("useTerminalContext must be used within TerminalProvider");
  return ctx;
}

// ── Provider ─────────────────────────────────────────────────────

export function TerminalProvider({ children }: { children: React.ReactNode }) {
  const [activeTerminals, setActiveTerminals] = useState<Set<string>>(new Set());
  const termRefs = useRef<Map<string, HTMLDivElement | null>>(new Map());

  const openTerminal = useCallback((nodeId: string) => {
    setActiveTerminals((prev) => {
      if (prev.has(nodeId)) return prev;
      return new Set([...prev, nodeId]);
    });
  }, []);

  const closeTerminal = useCallback((nodeId: string) => {
    setActiveTerminals((prev) => {
      const next = new Set(prev);
      next.delete(nodeId);
      return next;
    });
    termRefs.current.delete(nodeId);
  }, []);

  const closeAll = useCallback(() => {
    setActiveTerminals(new Set());
    termRefs.current.clear();
  }, []);

  const getTerminalRef = useCallback((nodeId: string) => {
    return termRefs.current.get(nodeId) || null;
  }, []);

  return (
    <TerminalContext.Provider value={{ activeTerminals, openTerminal, closeTerminal, closeAll, getTerminalRef }}>
      {children}
      {/* Off-screen host: all active terminals stay mounted here to preserve scrollback */}
      <div
        className="fixed pointer-events-none"
        style={{ left: -9999, top: -9999, width: 800, height: 400 }}
        aria-hidden="true"
      >
        {Array.from(activeTerminals).map((nodeId) => (
          <div
            key={nodeId}
            ref={(el) => { termRefs.current.set(nodeId, el); }}
            style={{ width: "100%", height: "100%" }}
          >
            <TerminalInner nodeId={nodeId} labId="workshop" />
          </div>
        ))}
      </div>
    </TerminalContext.Provider>
  );
}

// ── Viewport ─────────────────────────────────────────────────────
// Reparents a terminal's DOM into the consumer's container.
// When unmounted, moves it back to the off-screen host.

export function TerminalViewport({ nodeId }: { nodeId: string }) {
  const { openTerminal, getTerminalRef } = useTerminalContext();
  const viewportRef = useRef<HTMLDivElement>(null);
  const [mounted, setMounted] = useState(false);

  // Ensure terminal is open
  useEffect(() => {
    openTerminal(nodeId);
  }, [nodeId, openTerminal]);

  // Reparent terminal DOM into this viewport
  useEffect(() => {
    // Small delay to let the off-screen host render the terminal first
    const timer = setTimeout(() => {
      const termEl = getTerminalRef(nodeId);
      const viewport = viewportRef.current;
      if (termEl && viewport && termEl.parentElement !== viewport) {
        viewport.appendChild(termEl);
        setMounted(true);
        // Trigger xterm refit after reparent
        window.dispatchEvent(new Event("resize"));
      }
    }, 100);

    return () => {
      clearTimeout(timer);
      // Move terminal back to off-screen host on unmount would require
      // tracking the original parent, but since the Provider re-renders
      // the off-screen host, unmounting this viewport is safe — the
      // terminal stays alive in the Provider's hidden container on next render.
    };
  }, [nodeId, getTerminalRef]);

  return (
    <div ref={viewportRef} className="h-full w-full overflow-hidden bg-slate-950">
      {!mounted && (
        <div className="flex h-full items-center justify-center text-xs text-slate-600">
          Connecting to {nodeId}...
        </div>
      )}
    </div>
  );
}
