import { MetricsOverview } from "../../components/metrics-overview";
import { SubstationPanel } from "../../components/substation-panel";

export default function SubstationPage() {
  return (
    <main className="mx-auto flex w-full max-w-6xl flex-col gap-6 px-6 py-10">
      <header>
        <p className="text-sm uppercase tracking-[0.3em] text-slate-500">Process View</p>
        <h1 className="text-3xl font-bold text-white">Distribution Substation</h1>
        <p className="mt-1 text-sm text-slate-400">
          Live feeder state, device controls, and command audit trail
        </p>
      </header>

      <MetricsOverview />
      <SubstationPanel />
    </main>
  );
}
