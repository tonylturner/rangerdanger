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

  // Fit terminal to container
  const fitTerminal = useCallback(() => {
    if (fitAddonRef.current && terminalRef.current) {
      try {
        fitAddonRef.current.fit();
      } catch {
        // Ignore fit errors during unmount
      }
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
      fitAddon.fit();
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
      // Re-fit after connection to ensure proper sizing
      requestAnimationFrame(() => fitAddon.fit());
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
      requestAnimationFrame(() => fitAddon.fit());
    };
    window.addEventListener("resize", handleResize);

    // Use ResizeObserver for container size changes
    const resizeObserver = new ResizeObserver(() => {
      requestAnimationFrame(() => fitAddon.fit());
    });
    resizeObserver.observe(containerRef.current);

    return () => {
      window.removeEventListener("resize", handleResize);
      resizeObserver.disconnect();
      ws.close();
      term.dispose();
      terminalRef.current = null;
      fitAddonRef.current = null;
      wsRef.current = null;
    };
  }, [nodeId, labId, expanded]);

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
      <div
        className={`absolute right-2 top-2 z-10 h-2 w-2 rounded-full ${
          status === "connected"
            ? "bg-green-500"
            : status === "connecting"
            ? "bg-yellow-500 animate-pulse"
            : status === "error"
            ? "bg-red-500"
            : "bg-slate-500"
        }`}
      />
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
