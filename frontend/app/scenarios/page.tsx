import { ScenarioList } from "../../components/scenario-list";

export default function ScenariosPage() {
  return (
    <main className="mx-auto w-full max-w-4xl px-6 py-10">
      <div className="space-y-2">
        <h1 className="text-3xl font-semibold text-white">Scenarios</h1>
        <p className="text-slate-400">
          Build modular exercises with attacker playbooks, defender guidance, and automation hooks that run inside the lab
          networks.
        </p>
      </div>
      <div className="mt-6 space-y-4">
        <ScenarioList fetchAll />
      </div>
    </main>
  );
}
