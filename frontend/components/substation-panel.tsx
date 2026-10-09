"use client";

import { useState } from "react";
import { sendSubstationCommand, type AuditEntry, type NetworkEvent } from "../lib/api";
import { useRefreshLive, useSubstationAudit, useSubstationNetworkEvents, useSubstationState } from "../lib/live-queries";
import { isRangeNotReady } from "../lib/range";
import { errorMessage } from "../lib/utils";
import { OneLine } from "./substation-one-line";
import { CommandPanel } from "./substation-commands";
import { CommandAuditView } from "./substation-audit";
import { ElectricalDetailView } from "./substation-electrical";

// Stable empties so children memoising on these props do not recompute
// on every render before the first answer.
const NO_AUDIT: AuditEntry[] = [];
const NO_EVENTS: NetworkEvent[] = [];

export function SubstationPanel() {
  const state = useSubstationState(3000).data ?? null;
  const audit = useSubstationAudit(3000).data?.entries ?? NO_AUDIT;
  const networkEvents = useSubstationNetworkEvents(3000).data?.events ?? NO_EVENTS;
  const refresh = useRefreshLive();
  const [tab, setTab] = useState<"diagram" | "commands" | "correlation" | "electrical">("diagram");
  const [cmdResult, setCmdResult] = useState<string | null>(null);

  const elec = state?.electrical;
  const relay = state?.devices?.relay;
  const recloser = state?.devices?.recloser;
  const regulator = state?.devices?.regulator;
  const capbank = state?.devices?.capbank;

  const execCmd = async (device: string, command: string, value?: number) => {
    try {
      const res = await sendSubstationCommand(device, command, undefined, value);
      setCmdResult(`${res.result}: ${res.process_impact || res.detail}`);
      setTimeout(refresh, 500);
    } catch (e) {
      setCmdResult(isRangeNotReady(e) ? e.message : `Error: ${errorMessage(e)}`);
    }
  };

  const tabs = [
    { id: "diagram" as const, label: "Feeder One-Line" },
    { id: "commands" as const, label: "Supervisory Control" },
    { id: "correlation" as const, label: "Command Audit" },
    { id: "electrical" as const, label: "Electrical Detail" },
  ];

  return (
    <div className="rounded-xl border border-slate-800 bg-slate-950">
      <div className="flex border-b border-slate-800">
        {tabs.map((t) => (
          <button
            key={t.id}
            onClick={() => setTab(t.id)}
            className={`px-4 py-2 text-xs font-medium transition-colors ${
              tab === t.id
                ? "border-b-2 border-sky-500 text-sky-400"
                : "text-slate-500 hover:text-slate-300"
            }`}
          >
            {t.label}
          </button>
        ))}
      </div>

      <div className="p-4">
        {tab === "diagram" && (
          <OneLine elec={elec} relay={relay} recloser={recloser} regulator={regulator} capbank={capbank} />
        )}
        {tab === "commands" && (
          <CommandPanel
            relay={relay}
            recloser={recloser}
            regulator={regulator}
            capbank={capbank}
            execCmd={execCmd}
            cmdResult={cmdResult}
          />
        )}
        {tab === "correlation" && <CommandAuditView entries={audit} networkEvents={networkEvents} />}
        {tab === "electrical" && (
          <ElectricalDetailView
            elec={elec}
            relay={relay}
            recloser={recloser}
            regulator={regulator}
            capbank={capbank}
            physics={state?.physics}
            audit={audit}
          />
        )}
      </div>
    </div>
  );
}
