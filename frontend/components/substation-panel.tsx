"use client";

import { useEffect, useState, useCallback } from "react";
import {
  getSubstationState,
  getSubstationAudit,
  getSubstationNetworkEvents,
  sendSubstationCommand,
  type SubstationState,
  type AuditEntry,
  type NetworkEvent,
} from "../lib/api";
import { OneLine } from "./substation-one-line";
import { CommandPanel } from "./substation-commands";
import { CommandAuditView } from "./substation-audit";
import { ElectricalDetailView } from "./substation-electrical";

export function SubstationPanel() {
  const [state, setState] = useState<SubstationState | null>(null);
  const [audit, setAudit] = useState<AuditEntry[]>([]);
  const [networkEvents, setNetworkEvents] = useState<NetworkEvent[]>([]);
  const [tab, setTab] = useState<"diagram" | "commands" | "correlation" | "electrical">("diagram");
  const [cmdResult, setCmdResult] = useState<string | null>(null);

  const poll = useCallback(async () => {
    try {
      const [s, a, ne] = await Promise.all([
        getSubstationState(),
        getSubstationAudit(),
        getSubstationNetworkEvents(),
      ]);
      setState(s);
      setAudit(a.entries ?? []);
      setNetworkEvents(ne.events ?? []);
    } catch {
      // offline
    }
  }, []);

  useEffect(() => {
    poll();
    const id = setInterval(poll, 3000);
    return () => clearInterval(id);
  }, [poll]);

  const elec = state?.electrical;
  const relay = state?.devices?.relay;
  const recloser = state?.devices?.recloser;
  const regulator = state?.devices?.regulator;
  const capbank = state?.devices?.capbank;

  const execCmd = async (device: string, command: string, value?: number) => {
    try {
      const res = await sendSubstationCommand(device, command, undefined, value);
      setCmdResult(`${res.result}: ${res.process_impact || res.detail}`);
      setTimeout(poll, 500);
    } catch (e) {
      setCmdResult(`Error: ${e}`);
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
            audit={audit}
          />
        )}
      </div>
    </div>
  );
}
