"use client";

import Link from "next/link";
import { ArrowRightIcon } from "@radix-ui/react-icons";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { listLabInstances, startLabInstance, stopLabInstance } from "../lib/api";
import { Button } from "./ui/button";

export function LabsTable() {
  const queryClient = useQueryClient();
  const { data, isLoading } = useQuery({ queryKey: ["lab-instances"], queryFn: listLabInstances });

  const startMutation = useMutation({
    mutationFn: (labId: string) => startLabInstance(labId),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["lab-instances"] })
  });

  const stopMutation = useMutation({
    mutationFn: (labId: string) => stopLabInstance(labId),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["lab-instances"] })
  });

  if (isLoading) {
    return <div className="rounded-xl border border-slate-800 bg-slate-900/70 p-4">Loading labs...</div>;
  }

  return (
    <div className="overflow-hidden rounded-2xl border border-slate-800 bg-slate-950/60">
      <table className="w-full text-left text-sm">
        <thead className="bg-slate-900 text-slate-400">
          <tr>
            <th className="px-4 py-3 font-medium">Name</th>
            <th className="px-4 py-3 font-medium">Template</th>
            <th className="px-4 py-3 font-medium">Status</th>
            <th className="px-4 py-3 font-medium text-right">Actions</th>
          </tr>
        </thead>
        <tbody>
          {data?.instances.map((lab) => (
            <tr key={lab.id} className="border-t border-slate-900/80 text-slate-200">
              <td className="px-4 py-3">{lab.name || lab.id}</td>
              <td className="px-4 py-3 text-slate-400">{lab.template_id}</td>
              <td className="px-4 py-3">
                <span className="rounded-full bg-slate-800 px-3 py-1 text-xs uppercase tracking-wide text-slate-300">
                  {lab.status}
                </span>
              </td>
              <td className="px-4 py-3 text-right">
                <div className="flex items-center justify-end gap-2">
                  <Button
                    variant="ghost"
                    size="sm"
                    disabled={lab.status === "running" || startMutation.isLoading}
                    onClick={() => startMutation.mutate(lab.id)}
                  >
                    {startMutation.isLoading && startMutation.variables === lab.id ? "Starting..." : "Start"}
                  </Button>
                  <Button
                    variant="ghost"
                    size="sm"
                    disabled={lab.status !== "running" || stopMutation.isLoading}
                    onClick={() => stopMutation.mutate(lab.id)}
                  >
                    {stopMutation.isLoading && stopMutation.variables === lab.id ? "Stopping..." : "Stop"}
                  </Button>
                  <Button asChild variant="ghost" size="sm">
                    <Link href={`/labs/${lab.id}`}>
                      View <ArrowRightIcon className="ml-2" />
                    </Link>
                  </Button>
                </div>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
