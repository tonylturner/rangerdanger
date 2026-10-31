import Link from "next/link";

// Status and link cards shared by the overview and workshop-status
// pages of every package.

export function DashboardStatusCard({ label, value, ok }: { label: string; value: string; ok?: boolean }) {
  return (
    <div className={`rounded-lg border px-3 py-2 ${
      ok === false ? "border-red-800/60 bg-red-950/20" : ok === true ? "border-green-800/40 bg-slate-900/70" : "border-slate-800 bg-slate-900/70"
    }`}>
      <div className="text-[9px] font-medium uppercase tracking-wider text-slate-500">{label}</div>
      <div className={`text-sm font-bold ${
        ok === false ? "text-red-400" : ok === true ? "text-green-400" : "text-slate-400"
      }`}>
        {value}
      </div>
    </div>
  );
}

export function QuickLink({ href, title, desc, color }: { href: string; title: string; desc: string; color: string }) {
  const borderColor = color === "sky" ? "border-sky-800/50 hover:border-sky-700" : color === "orange" ? "border-orange-800/50 hover:border-orange-700" : "border-purple-800/50 hover:border-purple-700";
  const titleColor = color === "sky" ? "text-sky-400" : color === "orange" ? "text-orange-400" : "text-purple-400";
  return (
    <Link
      href={href}
      className={`block rounded-lg border bg-slate-950/50 p-3 transition-colors ${borderColor}`}
    >
      <div className={`text-sm font-bold ${titleColor}`}>{title}</div>
      <div className="mt-1 text-[11px] text-slate-500">{desc}</div>
    </Link>
  );
}

export function WorkshopStatusCard({ label, status, detail }: { label: string; status: "online" | "offline" | "degraded" | "waiting"; detail: string }) {
  const colors = {
    online: "border-green-800/60 bg-green-950/20 text-green-400",
    offline: "border-red-800/60 bg-red-950/20 text-red-400",
    degraded: "border-yellow-800/60 bg-yellow-950/20 text-yellow-400",
    waiting: "border-slate-800 bg-slate-900/40 text-slate-400",
  };
  const labels = { online: "ONLINE", offline: "OFFLINE", degraded: "DEGRADED", waiting: "WAITING" };
  return (
    <div className={`rounded-lg border px-3 py-2 ${colors[status]}`}>
      <div className="text-[9px] font-medium uppercase tracking-wider text-slate-500">{label}</div>
      <div className={`text-sm font-bold ${colors[status].split(" ").pop()}`}>{labels[status]}</div>
      <div className="text-[10px] text-slate-500 mt-0.5">{detail}</div>
    </div>
  );
}
