"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createLabInstance, listLabInstances, listLabTemplates } from "../lib/api";
import { Button } from "./ui/button";

export function LabsSummary() {
  const queryClient = useQueryClient();
  const { data: instances } = useQuery({ queryKey: ["lab-instances"], queryFn: listLabInstances });
  const { data: templates } = useQuery({ queryKey: ["lab-templates"], queryFn: listLabTemplates });

  const activeCount = instances?.instances.filter((lab) => lab.status === "running").length ?? 0;
  const totalCount = instances?.instances.length ?? 0;
  const defaultTemplateId = templates?.templates[0]?.id;

  const createLab = useMutation({
    mutationFn: (templateId: string) => {
      const label = `Lab ${new Date().toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}`;
      return createLabInstance(templateId, label);
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["lab-instances"] });
    }
  });

  return (
    <section className="rounded-2xl border border-slate-800 bg-slate-900/70 p-6">
      <div className="flex items-center justify-between">
        <div>
          <p className="text-sm uppercase tracking-wide text-slate-400">Active Labs</p>
          <h2 className="text-3xl font-semibold text-white">{activeCount}</h2>
          <p className="text-sm text-slate-400">{totalCount} total instances</p>
        </div>
        <div className="text-right">
          <p className="text-sm uppercase tracking-wide text-slate-400">Templates</p>
          <h3 className="text-2xl font-semibold text-white">{templates?.templates.length ?? 0}</h3>
          <Button
            className="mt-2"
            variant="outline"
            disabled={!defaultTemplateId || createLab.isLoading}
            onClick={() => defaultTemplateId && createLab.mutate(defaultTemplateId)}
          >
            {createLab.isLoading ? "Launching..." : "Start Default Lab"}
          </Button>
        </div>
      </div>
    </section>
  );
}
