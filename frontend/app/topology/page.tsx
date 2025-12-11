import { TopologyBuilder } from "../../components/topology-builder";

export default function TopologyBuilderPage() {
  return (
    <main className="mx-auto w-full max-w-5xl px-6 py-10">
      <header className="mb-6">
        <h1 className="text-3xl font-semibold text-white">Topology Builder</h1>
        <p className="text-slate-400">
          Drag node templates, assign networks, and push the JSON into backend LabTemplates.
        </p>
      </header>
      <TopologyBuilder />
    </main>
  );
}
