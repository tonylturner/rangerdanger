import { LabsSummary } from "../components/labs-summary";
import { LabsTable } from "../components/labs-table";
import { MetricsOverview } from "../components/metrics-overview";
import { TopologyPreview } from "../components/topology-preview";

export default function DashboardPage() {
  return (
    <main className="mx-auto flex w-full max-w-6xl flex-col gap-8 px-6 py-10">
      <header>
        <p className="text-sm uppercase tracking-[0.3em] text-slate-500">Dashboard</p>
        <h1 className="text-4xl font-bold text-white">OT Cyber Lab Trainer</h1>
        <p className="mt-2 max-w-2xl text-slate-400">
          Spin up realistic ICS topologies, launch guided attack/defense scenarios, and visualize telemetry
          in one modern control plane.
        </p>
      </header>

      <LabsSummary />
      <TopologyPreview />
      <MetricsOverview />
      <LabsTable />
    </main>
  );
}
