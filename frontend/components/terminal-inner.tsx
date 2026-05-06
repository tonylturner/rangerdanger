"use client";
import { useEffect, useRef, useState, useCallback } from "react";
import { Terminal } from "xterm";
import { FitAddon } from "xterm-addon-fit";
import "xterm/css/xterm.css";

type TerminalInnerProps = {
  nodeId: string;
  labId: string;
  expanded?: boolean;
};

export default function TerminalInner({ nodeId, labId, expanded = false }: TerminalInnerProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const termRef = useRef<HTMLDivElement>(null);
  const terminalRef = useRef<Terminal | null>(null);
  const fitAddonRef = useRef<FitAddon | null>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const [status, setStatus] = useState<"connecting" | "connected" | "disconnected" | "error">("connecting");
  const [connectKey, setConnectKey] = useState(0);

  const reconnect = useCallback(() => {
    setStatus("connecting");
    setConnectKey((k) => k + 1);
  }, []);

  // Fit terminal to container, send resize to server, and refresh the prompt.
  const fitTerminal = useCallback(() => {
    const term = terminalRef.current;
    const fit = fitAddonRef.current;
    const container = containerRef.current;
    const ws = wsRef.current;
    if (!term || !fit || !container) return;
    // Skip fit while container is hidden (display:none → 0 size). fit() at
    // zero dimensions corrupts xterm's cached column count.
    if (container.offsetWidth === 0 || container.offsetHeight === 0) return;
    try {
      fit.fit();
      // Tell the server (Docker exec or SSH) to resize its PTY so bash's
      // stty size matches and the prompt draws at the correct width.
      if (ws && ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ type: "resize", cols: term.cols, rows: term.rows }));
        // Redraw the current line (Ctrl+L clears screen; Ctrl+R then Ctrl+R
        // would redraw the prompt from history, but Ctrl+L is simpler and
        // bash/readline handles it correctly).
        term.refresh(0, term.rows - 1);
      }
    } catch {
      // Ignore fit errors during unmount
    }
  }, []);

  useEffect(() => {
    if (!termRef.current || !containerRef.current) return;

    const term = new Terminal({
      cursorBlink: true,
      theme: {
        background: "#0f172a",
        foreground: "#e2e8f0",
        cursor: "#22d3ee",
        selectionBackground: "#334155",
      },
      fontFamily: "ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace",
      fontSize: expanded ? 14 : 12,
      scrollback: 5000,
      convertEol: true,
      allowProposedApi: true,
    });

    const fitAddon = new FitAddon();
    term.loadAddon(fitAddon);

    terminalRef.current = term;
    fitAddonRef.current = fitAddon;

    term.open(termRef.current);

    // Initial fit after a brief delay to ensure container is rendered
    requestAnimationFrame(() => {
      fitTerminal();
    });

    term.writeln("\x1b[36mConnecting to container...\x1b[0m");

    const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
    const wsUrl = labId === "workshop"
      ? `${protocol}//${window.location.host}/api/workshop/nodes/${nodeId}/terminal`
      : `${protocol}//${window.location.host}/api/labs/instances/${labId}/nodes/${nodeId}/terminal`;
    const ws = new WebSocket(wsUrl);
    wsRef.current = ws;

    ws.onopen = () => {
      setStatus("connected");
      term.writeln("\x1b[32mConnected!\x1b[0m\r\n");
      // Re-fit after connection to ensure proper sizing and send the
      // initial resize so the remote PTY matches the local xterm.
      requestAnimationFrame(() => fitTerminal());
    };

    ws.onmessage = (e) => {
      if (e.data instanceof Blob) {
        e.data.text().then((text) => term.write(text));
      } else {
        term.write(e.data);
      }
    };

    ws.onclose = () => {
      setStatus("disconnected");
      term.writeln("\r\n\x1b[33m[Connection closed]\x1b[0m");
    };

    ws.onerror = () => {
      setStatus("error");
      term.writeln("\r\n\x1b[31m[Connection error]\x1b[0m");
    };

    term.onData((data) => {
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(data);
      }
    });

    // Handle window resize
    const handleResize = () => {
      requestAnimationFrame(() => fitTerminal());
    };
    window.addEventListener("resize", handleResize);

    // Use ResizeObserver for container size changes
    const resizeObserver = new ResizeObserver(() => {
      requestAnimationFrame(() => fitTerminal());
    });
    resizeObserver.observe(containerRef.current);

    // Use IntersectionObserver to detect when the panel becomes visible again
    // (e.g. after switching back from another node tab). ResizeObserver does
    // not always fire on display:none → display:block transitions, so this
    // catches the visibility change and triggers a re-fit + PTY resize.
    const intersectionObserver = new IntersectionObserver((entries) => {
      for (const entry of entries) {
        if (entry.isIntersecting && entry.intersectionRatio > 0) {
          requestAnimationFrame(() => fitTerminal());
        }
      }
    }, { threshold: [0, 0.01] });
    intersectionObserver.observe(containerRef.current);

    return () => {
      window.removeEventListener("resize", handleResize);
      resizeObserver.disconnect();
      intersectionObserver.disconnect();
      ws.close();
      term.dispose();
      terminalRef.current = null;
      fitAddonRef.current = null;
      wsRef.current = null;
    };
  }, [nodeId, labId, expanded, fitTerminal, connectKey]);

  // Re-fit when expanded changes
  useEffect(() => {
    fitTerminal();
  }, [expanded, fitTerminal]);

  return (
    <div
      ref={containerRef}
      className="relative h-full w-full overflow-hidden"
      style={{ minHeight: 0 }}
    >
      <div className="absolute right-2 top-2 z-10 flex items-center gap-2">
        {(status === "disconnected" || status === "error") && (
          <button
            onClick={reconnect}
            className="rounded border border-amber-700 bg-amber-950/80 px-2 py-0.5 text-[10px] font-medium text-amber-400 hover:bg-amber-900/80 transition-colors"
          >
            Reconnect
          </button>
        )}
        <div
          className={`h-2 w-2 rounded-full ${
            status === "connected"
              ? "bg-green-500"
              : status === "connecting"
              ? "bg-yellow-500 animate-pulse"
              : status === "error"
              ? "bg-red-500"
              : "bg-slate-500"
          }`}
        />
      </div>
      <div
        ref={termRef}
        className="h-full w-full bg-slate-950"
        style={{
          position: 'absolute',
          top: 0,
          left: 0,
          right: 0,
          bottom: 0,
          overflow: 'hidden'
        }}
      />
    </div>
  );
}
