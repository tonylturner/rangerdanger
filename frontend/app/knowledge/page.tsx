"use client";

import { KnowledgeReader } from "../../components/knowledge-reader";
import { useKnowledge } from "../../lib/knowledge-queries";

export default function KnowledgePage() {
  const { data, error } = useKnowledge();
  if (error) return <main className="p-6 text-slate-300" role="alert">Knowledge unavailable: {error.message}</main>;
  if (!data) return <main className="p-6 text-slate-300" role="status">Loading package knowledge…</main>;
  if (data.sections.length === 0) return <main className="p-6 text-slate-300" role="status">This package has no knowledge articles.</main>;
  return <KnowledgeReader key={`${data.package_id}:${data.revision}`} sections={data.sections} />;
}
