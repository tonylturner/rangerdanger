"use client";
import { useEffect, useRef, useState } from "react";
import { Terminal } from "xterm";
import { FitAddon } from "xterm-addon-fit";
import "xterm/css/xterm.css";

type TerminalInnerProps = {
  nodeId: string;
  labId: string;
};

export default function TerminalInner({ nodeId, labId }: TerminalInnerProps) {
  const termRef = useRef<HTMLDivElement>(null);
  const [status, setStatus] = useState<"connecting" | "connected" | "disconnected" | "error">("connecting");

  useEffect(() => {
    if (!termRef.current) return;

    const term = new Terminal({
      cursorBlink: true,
      theme: {
        background: "#0f172a",
        foreground: "#e2e8f0",
        cursor: "#22d3ee",
        selectionBackground: "#334155",
      },
      fontFamily: "ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace",
      fontSize: 13,
    });
    const fitAddon = new FitAddon();
    term.loadAddon(fitAddon);
    term.open(termRef.current);
    fitAddon.fit();

    term.writeln("\x1b[36mConnecting to container...\x1b[0m");

    const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
    const wsUrl = `${protocol}//${window.location.host}/api/labs/instances/${labId}/nodes/${nodeId}/terminal`;
    const ws = new WebSocket(wsUrl);

    ws.onopen = () => {
      setStatus("connected");
      term.writeln("\x1b[32mConnected!\x1b[0m\r\n");
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

    // Handle resize
    const handleResize = () => fitAddon.fit();
    window.addEventListener("resize", handleResize);

    return () => {
      window.removeEventListener("resize", handleResize);
      ws.close();
      term.dispose();
    };
  }, [nodeId, labId]);

  return (
    <div className="relative h-full">
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
      <div ref={termRef} className="h-full bg-slate-950" />
    </div>
  );
}
