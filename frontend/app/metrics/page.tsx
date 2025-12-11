import { MetricsOverview } from "../../components/metrics-overview";

export default function MetricsPage() {
  return (
    <main className="mx-auto w-full max-w-5xl px-6 py-10">
      <header className="mb-6">
        <h1 className="text-3xl font-semibold text-white">Telemetry</h1>
        <p className="text-slate-400">Visualize OT process metrics, alarms, and IDS events.</p>
      </header>
      <MetricsOverview />
    </main>
  );
}
