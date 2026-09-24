"use client";

// ── Supervisory Control Panel ────────────────────────────────────

export function CommandPanel({
  relay,
  recloser,
  regulator,
  capbank,
  execCmd,
  cmdResult,
}: {
  relay?: Record<string, number | boolean | string>;
  recloser?: Record<string, number | boolean | string>;
  regulator?: Record<string, number | boolean | string>;
  capbank?: Record<string, number | boolean | string>;
  execCmd: (device: string, command: string, value?: number) => void;
  cmdResult: string | null;
}) {
  // In AUTO the regulator holds its setpoint and the cap bank self-corrects, so
  // a direct manual command is ambiguous: the cap reverts a switch within a
  // cycle, and a small regulator tap inside the deadband can actually persist.
  // Rather than expose a button whose effect is inconsistent, disable the
  // manual actuators and point the operator at the mode toggle.
  const regManual = Boolean(regulator?.manual_mode);
  const capAuto = Boolean(capbank?.auto_mode);
  return (
    <div className="space-y-4">
      {/* SCADA → RTAC → field-device hierarchy - the RTAC is the controller;
          the four cards below are its peer feeder devices. */}
      <div className="flex flex-wrap items-center gap-2 rounded-lg border border-slate-800 bg-slate-900/30 px-3 py-2 text-[10px]">
        <span className="font-bold text-sky-400">SCADA / HMI</span>
        <span className="text-slate-600">→</span>
        <span className="font-bold text-orange-400">RTAC</span>
        <span className="text-slate-600">→</span>
        <span className="font-bold text-green-400">Field Devices</span>
        <span className="text-slate-600">· 10.40.40.0/24</span>
        <span className="ml-auto text-slate-600">RTAC relays operator commands to the peer feeder devices below</span>
      </div>

      {cmdResult && (
        <div className={`rounded border px-3 py-2 text-xs ${
          cmdResult.includes("executed") || cmdResult.includes("CLOSED") || cmdResult.includes("ENABLED")
            ? "border-green-800/60 bg-green-950/20 text-green-400"
            : cmdResult.includes("Error") || cmdResult.includes("rejected")
            ? "border-red-800/60 bg-red-950/20 text-red-400"
            : "border-slate-700 bg-slate-900 text-slate-300"
        }`}>
          {cmdResult}
        </div>
      )}

      <div className="grid gap-3 md:grid-cols-2">
        <DeviceGroup title="Feeder Breaker (52)" subtitle="10.40.40.20" role="Service interruption - energize / de-energize feeder">
          <CmdButton label="TRIP" onClick={() => execCmd("relay", "trip")} variant="danger" />
          <CmdButton label="CLOSE" onClick={() => execCmd("relay", "close")} variant="success" />
          <CmdButton label="Lockout" onClick={() => execCmd("relay", "lockout")} variant="warning" />
          <CmdButton label="Unlock" onClick={() => execCmd("relay", "unlock")} />
          <div className="w-full border-t border-slate-800/50 my-0.5" />
          <CmdButton label="Inject Fault" onClick={() => execCmd("relay", "inject_fault")} variant="danger" />
          <CmdButton label="Clear Fault" onClick={() => execCmd("relay", "clear_fault")} />
        </DeviceGroup>

        <DeviceGroup title="Recloser (79)" subtitle="10.40.40.21" role="Reliability - automatic fault recovery">
          <CmdButton label="OPEN" onClick={() => execCmd("recloser", "open")} variant="danger" />
          <CmdButton label="CLOSE" onClick={() => execCmd("recloser", "close")} variant="success" />
          <CmdButton label="Enable Reclose" onClick={() => execCmd("recloser", "enable_reclose")} variant="success" />
          <CmdButton label="Disable Reclose" onClick={() => execCmd("recloser", "disable_reclose")} variant="warning" />
          <CmdButton label="Reset Lockout" onClick={() => execCmd("recloser", "reset_lockout")} />
          <div className="w-full border-t border-slate-800/50 my-0.5" />
          <CmdButton label="Inject Fault" onClick={() => execCmd("recloser", "inject_fault")} variant="danger" />
          <CmdButton label="Clear Fault" onClick={() => execCmd("recloser", "clear_fault")} />
        </DeviceGroup>

        <DeviceGroup title="Capacitor Bank (CAP)" subtitle="10.40.40.23" role="Power factor - reactive support">
          <CmdButton label="Switch In" onClick={() => execCmd("capbank", "switch_in")} variant="success" disabled={capAuto} title={capAuto ? "Cap bank in AUTO - switch to Manual to control" : undefined} />
          <CmdButton label="Switch Out" onClick={() => execCmd("capbank", "switch_out")} variant="danger" disabled={capAuto} title={capAuto ? "Cap bank in AUTO - switch to Manual to control" : undefined} />
          <div className="w-full border-t border-slate-800/50 my-0.5" />
          <CmdButton label="Manual Mode" onClick={() => execCmd("capbank", "set_manual")} variant="warning" />
          <CmdButton label="Auto Mode" onClick={() => execCmd("capbank", "set_auto")} variant="success" />
          <CmdButton label="Reset Lockout" onClick={() => execCmd("capbank", "reset_lockout")} />
        </DeviceGroup>

        <DeviceGroup title="Voltage Regulator (90)" subtitle="10.40.40.22" role="Voltage control - tap regulation">
          <CmdButton label="Raise Tap" onClick={() => execCmd("regulator", "raise_tap")} disabled={!regManual} title={!regManual ? "Regulator in AUTO - switch to Manual to adjust taps" : undefined} />
          <CmdButton label="Lower Tap" onClick={() => execCmd("regulator", "lower_tap")} disabled={!regManual} title={!regManual ? "Regulator in AUTO - switch to Manual to adjust taps" : undefined} />
          <div className="w-full border-t border-slate-800/50 my-0.5" />
          <CmdButton label="Manual Mode" onClick={() => execCmd("regulator", "set_manual")} variant="warning" />
          <CmdButton label="Auto Mode" onClick={() => execCmd("regulator", "set_auto")} variant="success" />
        </DeviceGroup>
      </div>

      <div className="text-[10px] text-slate-600">
        The containd firewall controls which zones can reach these devices on 10.40.40.0/24 -
        an attacker who reaches that segment can drive any of them.
      </div>
    </div>
  );
}

function DeviceGroup({ title, subtitle, role, children }: { title: string; subtitle: string; role: string; children: React.ReactNode }) {
  return (
    <div className="rounded-lg border border-slate-800 bg-slate-900/50 p-3">
      <h4 className="text-xs font-bold text-slate-300">{title}</h4>
      <div className="text-[9px] text-slate-600">{subtitle}</div>
      <div className="mb-2 text-[9px] italic text-slate-500">{role}</div>
      <div className="flex flex-wrap gap-1.5">{children}</div>
    </div>
  );
}

function CmdButton({ label, onClick, variant, disabled, title }: { label: string; onClick: () => void; variant?: "danger" | "success" | "warning"; disabled?: boolean; title?: string }) {
  const colors = {
    danger: "border-red-800/60 bg-red-950/40 text-red-400 hover:bg-red-900/40",
    success: "border-green-800/60 bg-green-950/40 text-green-400 hover:bg-green-900/40",
    warning: "border-yellow-800/60 bg-yellow-950/40 text-yellow-400 hover:bg-yellow-900/40",
  };
  const cls = disabled
    ? "border-slate-800 bg-slate-900/30 text-slate-600 cursor-not-allowed"
    : variant ? colors[variant] : "border-slate-700 bg-slate-800/40 text-slate-300 hover:bg-slate-700/40";
  return (
    <button onClick={onClick} disabled={disabled} title={title} className={`rounded border px-2 py-1 text-[10px] font-medium transition-colors ${cls}`}>
      {label}
    </button>
  );
}
