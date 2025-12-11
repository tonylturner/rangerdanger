import { LabsTable } from "../../components/labs-table";

export default function LabsPage() {
  return (
    <main className="mx-auto w-full max-w-5xl px-6 py-10">
      <div className="mb-6">
        <h1 className="text-3xl font-semibold text-white">Labs</h1>
        <p className="text-slate-400">Manage lab instances derived from opinionated templates.</p>
      </div>
      <LabsTable />
    </main>
  );
}
