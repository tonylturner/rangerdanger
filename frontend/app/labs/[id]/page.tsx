import { LabDetail } from "../../../components/lab-detail";

export default function LabDetailPage({ params }: { params: { id: string } }) {
  return (
    <main className="mx-auto w-full max-w-6xl px-6 py-10">
      <LabDetail labId={params.id} />
    </main>
  );
}
