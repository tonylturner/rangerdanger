"use client";
import { useEffect, useState } from "react";

type Event = {
  id: string;
  timestamp: string;
  type: string;
  source: string;
  dest: string;
  protocol: string;
  src_port: number;
  dst_port: number;
  details: string;
  severity: string;
  zone: string;
};

type ActivityFeedProps = {
  labId: string;
  maxEvents?: number;
};

const severityColors: Record<string, string> = {
  info: "text-slate-400",
  warning: "text-yellow-400",
  critical: "text-red-400",
};

const typeColors: Record<string, string> = {
  connection: "text-blue-400",
  modbus: "text-orange-400",
  dns: "text-cyan-400",
  alert: "text-red-400",
};

export function ActivityFeed({ labId, maxEvents = 50 }: ActivityFeedProps) {
  const [events, setEvents] = useState<Event[]>([]);
  const [status, setStatus] = useState<"connecting" | "connected" | "error">("connecting");

  useEffect(() => {
    const url = `/api/labs/instances/${labId}/live-events`;
    const es = new EventSource(url);

    es.onopen = () => {
      setStatus("connected");
    };

    es.addEventListener("event", (e) => {
      try {
        const event = JSON.parse(e.data);
        setEvents((prev) => [event, ...prev].slice(0, maxEvents));
      } catch {
        // Ignore parse errors
      }
    });

    es.addEventListener("error", (e) => {
      try {
        const error = JSON.parse((e as any).data);
        console.error("Event stream error:", error.message);
      } catch {
        // Connection error
      }
      setStatus("error");
    });

    es.onerror = () => {
      setStatus("error");
    };

    return () => es.close();
  }, [labId, maxEvents]);

  const formatTime = (timestamp: string) => {
    try {
      const date = new Date(timestamp);
      return date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" });
    } catch {
      return "--:--:--";
    }
  };

  return (
    <div className="flex h-full flex-col rounded-xl border border-slate-800 bg-slate-950">
      <div className="flex items-center justify-between border-b border-slate-800 px-4 py-2">
        <h3 className="text-sm font-medium text-slate-300">Activity Feed</h3>
        <div className="flex items-center gap-2">
          <span
            className={`h-2 w-2 rounded-full ${
              status === "connected"
                ? "bg-green-500"
                : status === "connecting"
                ? "bg-yellow-500 animate-pulse"
                : "bg-red-500"
            }`}
          />
          <span className="text-xs text-slate-500">{events.length} events</span>
        </div>
      </div>
      <div className="flex-1 overflow-y-auto p-2 font-mono text-xs">
        {events.length === 0 ? (
          <div className="flex h-full items-center justify-center text-slate-500">
            {status === "connecting" ? "Connecting to event stream..." : "No events yet"}
          </div>
        ) : (
          <div className="space-y-1">
            {events.map((e, i) => (
              <div
                key={e.id || i}
                className={`rounded px-2 py-1 hover:bg-slate-900 ${severityColors[e.severity] || "text-slate-400"}`}
              >
                <span className="text-slate-500">[{formatTime(e.timestamp)}]</span>{" "}
                <span className={typeColors[e.type] || "text-slate-400"}>{e.type}</span>{" "}
                <span className="text-slate-300">
                  {e.source}
                  {e.src_port ? `:${e.src_port}` : ""} → {e.dest}
                  {e.dst_port ? `:${e.dst_port}` : ""}
                </span>
                {e.protocol && e.protocol !== "-" && (
                  <span className="text-purple-400"> [{e.protocol}]</span>
                )}
                <div className="ml-16 text-slate-500">{e.details}</div>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
