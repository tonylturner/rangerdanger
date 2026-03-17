import { MetricsOverview } from "../../components/metrics-overview";
import { SubstationPanel } from "../../components/substation-panel";

export default function SubstationPage() {
  return (
    <main className="mx-auto flex w-full max-w-6xl flex-col gap-6 px-6 py-10">
      <header>
        <p className="text-[10px] uppercase tracking-[0.3em] text-slate-600">
          Feeder 101 — Distribution Substation
        </p>
        <h1 className="text-2xl font-bold text-white">Substation Process View</h1>
        <p className="mt-1 text-sm text-slate-500">
          Live feeder state from RTAC via OpenDSS physics engine. Commands are forwarded through the RTAC to field devices.
        </p>
      </header>

      <MetricsOverview />
      <SubstationPanel />
    </main>
  );
}
