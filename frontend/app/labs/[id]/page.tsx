import { LabDetail } from "../../../components/lab-detail";

export default async function LabDetailPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return (
    <main className="mx-auto w-full max-w-6xl px-6 py-10">
      <LabDetail labId={id} />
    </main>
  );
}
