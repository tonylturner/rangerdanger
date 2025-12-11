"use client";

import { Line, LineChart, ResponsiveContainer, Tooltip, XAxis, YAxis, Area, AreaChart } from "recharts";

const tankLevels = [
  { time: "00:00", value: 60 },
  { time: "00:15", value: 58 },
  { time: "00:30", value: 55 },
  { time: "00:45", value: 62 },
  { time: "01:00", value: 66 }
];

const alertCounts = [
  { time: "00:00", alerts: 1 },
  { time: "00:15", alerts: 0 },
  { time: "00:30", alerts: 3 },
  { time: "00:45", alerts: 2 },
  { time: "01:00", alerts: 1 }
];

export function MetricsOverview() {
  return (
    <div className="grid gap-6 md:grid-cols-2">
      <div className="rounded-2xl border border-slate-800 bg-slate-900/70 p-4">
        <div className="mb-2 flex items-center justify-between text-sm text-slate-400">
          <span>Tank Level (%)</span>
          <span>Last hour</span>
        </div>
        <ResponsiveContainer width="100%" height={200}>
          <LineChart data={tankLevels}>
            <XAxis dataKey="time" stroke="#94a3b8" />
            <YAxis stroke="#94a3b8" domain={[40, 80]} />
            <Tooltip contentStyle={{ background: "#020617", border: "1px solid #1e293b" }} />
            <Line dataKey="value" stroke="#0ea5e9" strokeWidth={3} dot={false} />
          </LineChart>
        </ResponsiveContainer>
      </div>

      <div className="rounded-2xl border border-slate-800 bg-slate-900/70 p-4">
        <div className="mb-2 flex items-center justify-between text-sm text-slate-400">
          <span>IDS Alerts</span>
          <span>Last hour</span>
        </div>
        <ResponsiveContainer width="100%" height={200}>
          <AreaChart data={alertCounts}>
            <defs>
              <linearGradient id="alerts" x1="0" y1="0" x2="0" y2="1">
                <stop offset="5%" stopColor="#f97316" stopOpacity={0.8} />
                <stop offset="95%" stopColor="#f97316" stopOpacity={0} />
              </linearGradient>
            </defs>
            <XAxis dataKey="time" stroke="#94a3b8" />
            <YAxis stroke="#94a3b8" />
            <Tooltip contentStyle={{ background: "#020617", border: "1px solid #1e293b" }} />
            <Area type="monotone" dataKey="alerts" stroke="#f97316" fillOpacity={1} fill="url(#alerts)" />
          </AreaChart>
        </ResponsiveContainer>
      </div>
    </div>
  );
}
