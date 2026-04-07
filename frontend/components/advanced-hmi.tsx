"use client";

import { useEffect, useState, useCallback, useRef } from "react";
import {
  getSubstationState,
  getSubstationAudit,
  sendSubstationCommand,
  type SubstationState,
  type AuditEntry,
} from "../lib/api";

/* ─────────────────────────────────────────────────────────────────
   SCADA HMI — Distribution Substation Feeder 101
   Full-screen SVG one-line diagram with inline measurements,
   click-to-control device popups, animated power flow, and
   real-time alarm annunciation.
   ───────────────────────────────────────────────────────────── */

// ── Color Constants ──────────────────────────────────────────────

const C = {
  energized: "#22d3ee",       // cyan-400 — live bus / path
  energizedGlow: "#06b6d4",   // cyan-500
  deenergized: "#64748b",     // slate-500 — dead path
  dead: "#334155",            // slate-700 — fully dead
  alarm: "#ef4444",           // red-500
  alarmGlow: "#dc2626",       // red-600
  warning: "#f59e0b",         // amber-500
  ok: "#22c55e",              // green-500
  bg: "#020617",              // slate-950
  panel: "#0f172a",           // slate-900
  panelBorder: "#1e293b",     // slate-800
  text: "#e2e8f0",            // slate-200
  textDim: "#64748b",         // slate-500
  textMuted: "#475569",       // slate-600
  measurement: "#fbbf24",     // amber-300 — measurement readouts
};

// ── Types ────────────────────────────────────────────────────────

type DeviceId = "relay" | "recloser" | "regulator";

type ControlPopup = {
  device: DeviceId;
  x: number;
  y: number;
};

// ── Main Component ───────────────────────────────────────────────

export function AdvancedHmi() {
  const [state, setState] = useState<SubstationState | null>(null);
  const [audit, setAudit] = useState<AuditEntry[]>([]);
  const [popup, setPopup] = useState<ControlPopup | null>(null);
  const [cmdFeedback, setCmdFeedback] = useState<string | null>(null);
  const [pollCount, setPollCount] = useState(0);
  const svgRef = useRef<SVGSVGElement>(null);

  const poll = useCallback(async () => {
    try {
      const [s, a] = await Promise.all([
        getSubstationState(),
        getSubstationAudit(),
      ]);
      setState(s);
      setAudit(a.entries ?? []);
      setPollCount((c) => c + 1);
    } catch {
      /* offline */
    }
  }, []);

  useEffect(() => {
    poll();
    const id = setInterval(poll, 2000);
    return () => clearInterval(id);
  }, [poll]);

  const execCmd = async (device: string, command: string, value?: number) => {
    try {
      const res = await sendSubstationCommand(device, command, "hmi-advanced", value);
      setCmdFeedback(`${device.toUpperCase()}: ${res.result} — ${res.process_impact || res.detail}`);
      setTimeout(() => setCmdFeedback(null), 4000);
      setTimeout(poll, 300);
    } catch (e) {
      setCmdFeedback(`ERROR: ${e}`);
      setTimeout(() => setCmdFeedback(null), 5000);
    }
    setPopup(null);
  };

  const openPopup = (device: DeviceId, x: number, y: number) => {
    setPopup(popup?.device === device ? null : { device, x, y });
  };

  // ── Derived state ──────────────────────────────────────────────

  const elec = state?.electrical;
  const relay = state?.devices?.relay;
  const recloser = state?.devices?.recloser;
  const regulator = state?.devices?.regulator;
  const comms = state?.device_comms;

  const bkrClosed = elec?.breaker_closed ?? false;
  const rclClosed = elec?.recloser_closed ?? false;
  const tap = elec?.regulator_tap ?? 0;
  const genEnergized = elec?.general_load_energized ?? false;
  const critEnergized = elec?.critical_load_energized ?? false;
  const busV = elec?.substation_bus_voltage_v ?? 0;
  const downV = elec?.downstream_voltage_v ?? 0;
  const critV = elec?.critical_load_voltage_v ?? 0;
  const feederA = elec?.feeder_current_a ?? 0;
  const genKw = elec?.general_load_kw ?? 0;
  const critKw = elec?.critical_load_kw ?? 0;
  const totalKw = genKw + critKw;
  const losses = elec?.total_losses_kw ?? 0;
  const pf = elec?.power_factor ?? 0;
  const sourceKw = elec?.source_power_kw ?? 0;

  // Alarm conditions
  const alarms: string[] = [];
  if (!bkrClosed) alarms.push("FEEDER BKR OPEN");
  if (bkrClosed && !rclClosed) alarms.push("RECLOSER OPEN");
  if (relay?.lockout) alarms.push("BKR LOCKOUT");
  if (recloser?.lockout) alarms.push("RCL LOCKOUT");
  if (relay?.fault_seen) alarms.push("FAULT DETECTED");
  if (recloser && !recloser.reclose_enabled) alarms.push("AUTO-RECLOSE OFF");
  if (critV > 0 && critV < 114) alarms.push("LOW VOLTAGE");
  if (critV > 126) alarms.push("HIGH VOLTAGE");
  if (comms && !comms.relay) alarms.push("RELAY COMMS FAIL");
  if (comms && !comms.recloser) alarms.push("RECLOSER COMMS FAIL");
  if (comms && !comms.regulator) alarms.push("REG COMMS FAIL");

  const hasAlarm = alarms.length > 0;

  // Path energization
  const busLive = true; // bus is always energized from source
  const postBkr = bkrClosed;
  const postRcl = bkrClosed && rclClosed;

  return (
    <div
      className="relative flex h-full flex-col overflow-hidden"
      style={{ background: C.bg }}
      onClick={() => setPopup(null)}
    >
      {/* ── Scanline overlay for CRT feel ───────────────────────── */}
      <div
        className="pointer-events-none absolute inset-0 z-50"
        style={{
          backgroundImage: `repeating-linear-gradient(
            0deg,
            transparent,
            transparent 2px,
            rgba(0,0,0,0.03) 2px,
            rgba(0,0,0,0.03) 4px
          )`,
        }}
      />

      {/* ── Top: Title Bar + Alarm Annunciator ──────────────────── */}
      <div
        className="flex items-center gap-4 border-b px-4 py-1.5"
        style={{ borderColor: C.panelBorder, background: C.panel }}
      >
        <div className="flex items-center gap-3">
          <div
            className="h-2.5 w-2.5 rounded-full"
            style={{
              background: hasAlarm ? C.alarm : C.ok,
              boxShadow: hasAlarm
                ? `0 0 8px ${C.alarm}`
                : `0 0 6px ${C.ok}`,
              animation: hasAlarm ? "hmi-pulse 1s ease-in-out infinite" : undefined,
            }}
          />
          <span
            className="text-xs font-bold tracking-wider"
            style={{
              fontFamily: "'JetBrains Mono', 'Fira Code', 'SF Mono', monospace",
              color: C.text,
            }}
          >
            FEEDER 101 — DISTRIBUTION SUBSTATION
          </span>
        </div>

        {/* Alarm ticker */}
        <div className="flex flex-1 items-center gap-2 overflow-hidden">
          {hasAlarm ? (
            alarms.map((a, i) => (
              <span
                key={i}
                className="shrink-0 rounded px-2 py-0.5 text-[10px] font-bold tracking-wide"
                style={{
                  background: "rgba(239,68,68,0.15)",
                  border: "1px solid rgba(239,68,68,0.4)",
                  color: C.alarm,
                  animation: "hmi-pulse 1.5s ease-in-out infinite",
                  animationDelay: `${i * 0.2}s`,
                  fontFamily: "'JetBrains Mono', monospace",
                }}
              >
                {a}
              </span>
            ))
          ) : (
            <span
              className="text-[10px] font-medium tracking-wide"
              style={{ color: C.ok, fontFamily: "'JetBrains Mono', monospace" }}
            >
              ALL NORMAL
            </span>
          )}
        </div>

        <span
          className="text-[10px] tabular-nums"
          style={{ color: C.textDim, fontFamily: "'JetBrains Mono', monospace" }}
        >
          {state?.last_poll
            ? new Date(state.last_poll).toLocaleTimeString()
            : "--:--:--"}
        </span>
      </div>

      {/* ── Command feedback toast ──────────────────────────────── */}
      {cmdFeedback && (
        <div
          className="absolute left-1/2 top-14 z-40 -translate-x-1/2 rounded-md border px-4 py-2 text-xs font-bold shadow-2xl"
          style={{
            background: cmdFeedback.includes("ERROR")
              ? "rgba(239,68,68,0.2)"
              : "rgba(34,197,94,0.15)",
            borderColor: cmdFeedback.includes("ERROR")
              ? "rgba(239,68,68,0.5)"
              : "rgba(34,197,94,0.4)",
            color: cmdFeedback.includes("ERROR") ? C.alarm : C.ok,
            fontFamily: "'JetBrains Mono', monospace",
          }}
        >
          {cmdFeedback}
        </div>
      )}

      {/* ── Main SVG One-Line Diagram ───────────────────────────── */}
      <div className="relative flex-1">
        <svg
          ref={svgRef}
          viewBox="0 0 1200 500"
          className="h-full w-full"
          preserveAspectRatio="xMidYMid meet"
          style={{ fontFamily: "'JetBrains Mono', 'Fira Code', monospace" }}
          onClick={(e) => e.stopPropagation()}
        >
          <defs>
            <filter id="glow-cyan" x="-50%" y="-50%" width="200%" height="200%">
              <feGaussianBlur stdDeviation="3" result="blur" />
              <feMerge>
                <feMergeNode in="blur" />
                <feMergeNode in="SourceGraphic" />
              </feMerge>
            </filter>
            <filter id="glow-red" x="-50%" y="-50%" width="200%" height="200%">
              <feGaussianBlur stdDeviation="4" result="blur" />
              <feColorMatrix in="blur" type="matrix" values="1 0 0 0 0  0 0.2 0 0 0  0 0 0.2 0 0  0 0 0 1 0" />
              <feMerge>
                <feMergeNode />
                <feMergeNode in="SourceGraphic" />
              </feMerge>
            </filter>
            <filter id="glow-green" x="-50%" y="-50%" width="200%" height="200%">
              <feGaussianBlur stdDeviation="3" result="blur" />
              <feMerge>
                <feMergeNode in="blur" />
                <feMergeNode in="SourceGraphic" />
              </feMerge>
            </filter>
            <filter id="glow-amber" x="-50%" y="-50%" width="200%" height="200%">
              <feGaussianBlur stdDeviation="2" result="blur" />
              <feMerge>
                <feMergeNode in="blur" />
                <feMergeNode in="SourceGraphic" />
              </feMerge>
            </filter>
          </defs>

          {/* ── Background grid ─────────────────────────────────── */}
          <pattern id="grid" width="40" height="40" patternUnits="userSpaceOnUse">
            <path d="M 40 0 L 0 0 0 40" fill="none" stroke={C.dead} strokeWidth="0.3" opacity="0.3" />
          </pattern>
          <rect width="1200" height="500" fill="url(#grid)" />

          {/* ── Zone labels ─────────────────────────────────────── */}
          <text x="60" y="22" fill={C.textDim} fontSize="9" fontWeight="bold" letterSpacing="2">
            OT OPERATIONS ZONE — 10.30.30.0/24
          </text>
          <text x="60" y="490" fill={C.textDim} fontSize="9" fontWeight="bold" letterSpacing="2">
            FIELD ZONE — 10.40.40.0/24
          </text>

          {/* ════════════════════════════════════════════════════════
              MAIN BUS — Horizontal line at top
              ════════════════════════════════════════════════════════ */}
          <g>
            <text x="60" y="62" fill={C.textDim} fontSize="10" fontWeight="bold">12.47 kV</text>
            <text x="60" y="74" fill={C.textDim} fontSize="9">SOURCE</text>

            <circle cx="130" cy="65" r="14" fill="none" stroke={C.energized} strokeWidth="2" filter="url(#glow-cyan)" />
            <text x="130" y="69" textAnchor="middle" fill={C.energized} fontSize="11" fontWeight="bold">G</text>

            <line x1="148" y1="65" x2="350" y2="65" stroke={C.energized} strokeWidth="4" filter="url(#glow-cyan)" />

            <MeasurementBox x={200} y={32} label="BUS" value={`${busV.toFixed(0)}V`} unit="" live={busLive} />
            <MeasurementBox x={280} y={32} label="SRC" value={`${sourceKw.toFixed(0)}`} unit="kW" live={busLive} />

            {busLive && <FlowParticles x1={148} y1={65} x2={350} y2={65} />}
          </g>

          {/* ════════════════════════════════════════════════════════
              FEEDER BREAKER (52) — Relay
              ════════════════════════════════════════════════════════ */}
          <g>
            <line x1="350" y1="65" x2="350" y2="110" stroke={busLive ? C.energized : C.deenergized} strokeWidth="3" filter={busLive ? "url(#glow-cyan)" : undefined} />
            {busLive && <FlowParticles x1={350} y1={65} x2={350} y2={108} vertical />}

            <g className="cursor-pointer" onClick={(e) => { e.stopPropagation(); openPopup("relay", 350, 145); }}>
              <circle cx="350" cy="130" r="18" fill={bkrClosed ? "rgba(34,211,238,0.08)" : "rgba(239,68,68,0.1)"} stroke={bkrClosed ? C.energized : C.alarm} strokeWidth="2.5" filter={bkrClosed ? "url(#glow-cyan)" : "url(#glow-red)"} />
              {bkrClosed ? (
                <line x1="350" y1="115" x2="350" y2="145" stroke={C.energized} strokeWidth="2.5" />
              ) : (
                <>
                  <line x1="350" y1="115" x2="350" y2="123" stroke={C.alarm} strokeWidth="2.5" />
                  <line x1="350" y1="123" x2="363" y2="137" stroke={C.alarm} strokeWidth="2.5" />
                  <line x1="350" y1="137" x2="350" y2="145" stroke={C.alarm} strokeWidth="2.5" />
                </>
              )}
              <text x="378" y="127" fill={C.text} fontSize="11" fontWeight="bold">52</text>
              <text x="378" y="140" fill={C.textDim} fontSize="9">BREAKER</text>
              {relay?.lockout && (
                <text x="378" y="151" fill={C.alarm} fontSize="8" fontWeight="bold" style={{ animation: "hmi-pulse 1s ease-in-out infinite" }}>LOCKOUT</text>
              )}
              {relay?.fault_seen && (
                <circle cx="334" cy="116" r="4" fill={C.alarm} filter="url(#glow-red)">
                  <animate attributeName="opacity" values="1;0.3;1" dur="0.8s" repeatCount="indefinite" />
                </circle>
              )}
              <text x="378" y={relay?.lockout ? 162 : 151} fill={C.textMuted} fontSize="8">10.40.40.20</text>
            </g>

            <line x1="350" y1="148" x2="350" y2="195" stroke={postBkr ? C.energized : C.deenergized} strokeWidth="3" strokeDasharray={postBkr ? undefined : "6 4"} filter={postBkr ? "url(#glow-cyan)" : undefined} />
            {postBkr && <FlowParticles x1={350} y1={150} x2={350} y2={193} vertical />}

            <MeasurementBox x={260} y={160} label="FDR" value={`${feederA.toFixed(1)}`} unit="A" live={postBkr} />
            <MeasurementBox x={260} y={195} label="DN" value={`${downV.toFixed(0)}`} unit="V" live={postBkr} warning={postBkr && (downV < 114 || downV > 126)} />
          </g>

          {/* ════════════════════════════════════════════════════════
              RECLOSER (79)
              ════════════════════════════════════════════════════════ */}
          <g>
            <line x1="350" y1="195" x2="350" y2="230" stroke={postBkr ? C.energized : C.deenergized} strokeWidth="3" strokeDasharray={postBkr ? undefined : "6 4"} filter={postBkr ? "url(#glow-cyan)" : undefined} />
            {postBkr && <FlowParticles x1={350} y1={197} x2={350} y2={228} vertical />}

            <g className="cursor-pointer" onClick={(e) => { e.stopPropagation(); openPopup("recloser", 350, 260); }}>
              <circle cx="350" cy="250" r="18" fill={rclClosed ? "rgba(34,211,238,0.08)" : "rgba(239,68,68,0.1)"} stroke={rclClosed ? C.energized : C.alarm} strokeWidth="2.5" filter={rclClosed ? "url(#glow-cyan)" : "url(#glow-red)"} />
              {rclClosed ? (
                <line x1="350" y1="235" x2="350" y2="265" stroke={C.energized} strokeWidth="2.5" />
              ) : (
                <>
                  <line x1="350" y1="235" x2="350" y2="243" stroke={C.alarm} strokeWidth="2.5" />
                  <line x1="350" y1="243" x2="363" y2="257" stroke={C.alarm} strokeWidth="2.5" />
                  <line x1="350" y1="257" x2="350" y2="265" stroke={C.alarm} strokeWidth="2.5" />
                </>
              )}
              <text x="378" y="246" fill={C.text} fontSize="11" fontWeight="bold">79</text>
              <text x="378" y="259" fill={C.textDim} fontSize="9">RECLOSER</text>
              <text x="378" y="271" fill={recloser?.reclose_enabled ? C.ok : C.warning} fontSize="8" fontWeight="bold">
                {recloser?.reclose_enabled ? "AR: ON" : "AR: OFF"}
              </text>
              <text x="378" y="282" fill={C.textMuted} fontSize="8">SHOTS: {String(recloser?.shot_count ?? 0)}/3</text>
              {recloser?.lockout && (
                <text x="378" y="293" fill={C.alarm} fontSize="8" fontWeight="bold">LOCKOUT</text>
              )}
              <text x="378" y={recloser?.lockout ? 304 : 293} fill={C.textMuted} fontSize="8">10.40.40.21</text>
            </g>
          </g>

          {/* ════════════════════════════════════════════════════════
              POST-RECLOSER — Branch to loads
              ════════════════════════════════════════════════════════ */}
          <g>
            <line x1="350" y1="268" x2="350" y2="330" stroke={postRcl ? C.energized : C.deenergized} strokeWidth="3" strokeDasharray={postRcl ? undefined : "6 4"} filter={postRcl ? "url(#glow-cyan)" : undefined} />
            {postRcl && <FlowParticles x1={350} y1={270} x2={350} y2={328} vertical />}

            <circle cx="350" cy="330" r="4" fill={postRcl ? C.energized : C.deenergized} filter={postRcl ? "url(#glow-cyan)" : undefined} />

            {/* ── LEFT BRANCH — General Load ────────────────────── */}
            <line x1="350" y1="330" x2="200" y2="330" stroke={postRcl ? C.energized : C.deenergized} strokeWidth="2.5" strokeDasharray={postRcl ? undefined : "6 4"} filter={postRcl ? "url(#glow-cyan)" : undefined} />
            <line x1="200" y1="330" x2="200" y2="385" stroke={postRcl ? C.energized : C.deenergized} strokeWidth="2.5" strokeDasharray={postRcl ? undefined : "6 4"} filter={postRcl ? "url(#glow-cyan)" : undefined} />
            {postRcl && <FlowParticles x1={340} y1={330} x2={200} y2={330} />}
            {postRcl && <FlowParticles x1={200} y1={333} x2={200} y2={383} vertical />}

            <g>
              <polygon points="187,388 213,388 206,412 194,412" fill={genEnergized ? "rgba(34,211,238,0.12)" : "rgba(100,116,139,0.1)"} stroke={genEnergized ? C.energized : C.deenergized} strokeWidth="2" filter={genEnergized ? "url(#glow-cyan)" : undefined} />
              <line x1="194" y1="416" x2="206" y2="416" stroke={genEnergized ? C.energized : C.deenergized} strokeWidth="2" />
              <line x1="196" y1="420" x2="204" y2="420" stroke={genEnergized ? C.energized : C.deenergized} strokeWidth="1.5" />
              <text x="200" y="438" textAnchor="middle" fill={C.text} fontSize="10" fontWeight="bold">GENERAL LOAD</text>
              <text x="200" y="452" textAnchor="middle" fill={genEnergized ? C.ok : C.alarm} fontSize="11" fontWeight="bold" filter={genEnergized ? "url(#glow-green)" : "url(#glow-red)"}>
                {genEnergized ? `${genKw} kW` : "NO POWER"}
              </text>
              <text x="200" y="466" textAnchor="middle" fill={C.textMuted} fontSize="8">~{Math.round(genKw * 3)} customers</text>
            </g>

            {/* ── RIGHT BRANCH — Regulator + Critical Load ──────── */}
            <line x1="350" y1="330" x2="580" y2="330" stroke={postRcl ? C.energized : C.deenergized} strokeWidth="2.5" strokeDasharray={postRcl ? undefined : "6 4"} filter={postRcl ? "url(#glow-cyan)" : undefined} />
            {postRcl && <FlowParticles x1={360} y1={330} x2={580} y2={330} />}

            <line x1="580" y1="330" x2="580" y2="350" stroke={postRcl ? C.energized : C.deenergized} strokeWidth="2.5" strokeDasharray={postRcl ? undefined : "6 4"} />

            {/* VOLTAGE REGULATOR SYMBOL — autotransformer */}
            <g className="cursor-pointer" onClick={(e) => { e.stopPropagation(); openPopup("regulator", 580, 380); }}>
              <circle cx="580" cy="366" r="14" fill="none" stroke={postRcl ? C.energized : C.deenergized} strokeWidth="2" filter={postRcl ? "url(#glow-cyan)" : undefined} />
              <circle cx="580" cy="384" r="14" fill="none" stroke={postRcl ? C.energized : C.deenergized} strokeWidth="2" filter={postRcl ? "url(#glow-cyan)" : undefined} />
              <line x1="596" y1="378" x2="606" y2={378 - tap * 0.7} stroke={C.measurement} strokeWidth="1.5" />
              <polygon points={`${604},${376 - tap * 0.7} ${608},${376 - tap * 0.7} ${606},${373 - tap * 0.7}`} fill={C.measurement} />

              <text x="616" y="365" fill={C.text} fontSize="11" fontWeight="bold">90</text>
              <text x="616" y="378" fill={C.textDim} fontSize="9">REGULATOR</text>
              <text x="616" y="391" fill={C.measurement} fontSize="10" fontWeight="bold" filter="url(#glow-amber)">TAP {tap > 0 ? "+" : ""}{tap}</text>
              <text x="616" y="402" fill={regulator?.manual_mode ? C.warning : C.ok} fontSize="8" fontWeight="bold">{regulator?.manual_mode ? "MANUAL" : "AUTO"}</text>
              <text x="616" y="413" fill={C.textMuted} fontSize="8">10.40.40.22</text>
            </g>

            {/* Post-regulator to critical load */}
            <line x1="580" y1="398" x2="580" y2="412" stroke={critEnergized ? C.energized : C.deenergized} strokeWidth="2.5" strokeDasharray={critEnergized ? undefined : "6 4"} />
            {critEnergized && <FlowParticles x1={580} y1={400} x2={580} y2={412} vertical />}

            <g>
              <polygon points="567,415 593,415 586,439 574,439" fill={critEnergized ? "rgba(34,211,238,0.12)" : "rgba(100,116,139,0.1)"} stroke={critEnergized ? C.energized : C.deenergized} strokeWidth="2" filter={critEnergized ? "url(#glow-cyan)" : undefined} />
              <text x="580" y="432" textAnchor="middle" fill={critEnergized ? C.warning : C.deenergized} fontSize="12" fontWeight="bold">!</text>
              <line x1="574" y1="443" x2="586" y2="443" stroke={critEnergized ? C.energized : C.deenergized} strokeWidth="2" />
              <line x1="576" y1="447" x2="584" y2="447" stroke={critEnergized ? C.energized : C.deenergized} strokeWidth="1.5" />

              <text x="580" y="464" textAnchor="middle" fill={C.text} fontSize="10" fontWeight="bold">CRITICAL LOAD</text>
              <text x="580" y="477" textAnchor="middle" fill={critEnergized ? C.ok : C.alarm} fontSize="11" fontWeight="bold" filter={critEnergized ? "url(#glow-green)" : "url(#glow-red)"}>
                {critEnergized ? `${critKw} kW` : "NO POWER"}
              </text>
              <text x="580" y="490" textAnchor="middle" fill={C.textMuted} fontSize="8">Hospital / Fire Station</text>

              <MeasurementBox x={480} y={435} label="CRIT" value={`${critV.toFixed(0)}`} unit="V" live={critEnergized} warning={critEnergized && (critV < 114 || critV > 126)} danger={critEnergized && (critV < 108 || critV > 132)} />
            </g>
          </g>

          {/* ════════════════════════════════════════════════════════
              RIGHT PANEL — System Metrics
              ════════════════════════════════════════════════════════ */}
          <g>
            <rect x="820" y="30" width="340" height="200" rx="4" fill={C.panel} stroke={C.panelBorder} strokeWidth="1" opacity="0.9" />
            <text x="840" y="52" fill={C.textDim} fontSize="9" fontWeight="bold" letterSpacing="2">SYSTEM METRICS</text>

            <BarGauge x={840} y={64} width={300} label="BUS VOLTAGE" value={busV} min={0} max={150} unit="V" nominal={120} warnLow={114} warnHigh={126} />
            <BarGauge x={840} y={96} width={300} label="FEEDER CURRENT" value={feederA} min={0} max={100} unit="A" nominal={30} warnHigh={60} />
            <BarGauge x={840} y={128} width={300} label="TOTAL LOAD" value={totalKw} min={0} max={500} unit="kW" nominal={200} />
            <BarGauge x={840} y={160} width={300} label="LOSSES" value={losses} min={0} max={50} unit="kW" nominal={10} warnHigh={25} />
            <BarGauge x={840} y={192} width={300} label="POWER FACTOR" value={pf * 100} min={0} max={100} unit="%" nominal={95} warnLow={85} />
          </g>

          {/* ════════════════════════════════════════════════════════
              RIGHT PANEL — Recent Commands
              ════════════════════════════════════════════════════════ */}
          <g>
            <rect x="820" y="248" width="340" height="240" rx="4" fill={C.panel} stroke={C.panelBorder} strokeWidth="1" opacity="0.9" />
            <text x="840" y="270" fill={C.textDim} fontSize="9" fontWeight="bold" letterSpacing="2">COMMAND LOG</text>

            {audit.slice(0, 8).map((entry, i) => {
              const isRecent = i === 0;
              const y = 288 + i * 24;
              return (
                <g key={i} opacity={1 - i * 0.08}>
                  <text x="840" y={y} fill={C.textMuted} fontSize="8">
                    {new Date(entry.timestamp).toLocaleTimeString()}
                  </text>
                  <text x="910" y={y} fill={isRecent ? C.measurement : C.textDim} fontSize="8" fontWeight={isRecent ? "bold" : "normal"}>
                    {entry.source}
                  </text>
                  <text x="980" y={y} fill={C.text} fontSize="8">{entry.command}</text>
                  <text x="1060" y={y} fill={entry.result === "executed" ? C.ok : C.alarm} fontSize="8">{entry.result}</text>
                </g>
              );
            })}
          </g>

          {/* ── Control Popup ───────────────────────────────────── */}
          {popup && (
            <DeviceControlPopup
              popup={popup}
              relay={relay}
              recloser={recloser}
              regulator={regulator}
              execCmd={execCmd}
            />
          )}
        </svg>
      </div>

      {/* ── Bottom Status Bar ───────────────────────────────────── */}
      <div
        className="flex items-center gap-6 border-t px-4 py-1"
        style={{
          borderColor: C.panelBorder,
          background: C.panel,
          fontFamily: "'JetBrains Mono', monospace",
        }}
      >
        <StatusIndicator label="RTAC" ok={!!state} />
        <StatusIndicator label="RELAY" ok={!!comms?.relay} />
        <StatusIndicator label="RECLOSER" ok={!!comms?.recloser} />
        <StatusIndicator label="REGULATOR" ok={!!comms?.regulator} />
        <div className="flex-1" />
        <span className="text-[9px] tabular-nums" style={{ color: C.textMuted }}>
          POLL #{pollCount}
        </span>
        <span className="text-[9px]" style={{ color: C.textMuted }}>
          {totalKw > 0
            ? `SERVING ~${Math.round(totalKw * 3)} CUSTOMERS`
            : "ALL CUSTOMERS OUT"}
        </span>
      </div>

      {/* ── Keyframe animations ─────────────────────────────────── */}
      <style jsx global>{`
        @keyframes hmi-pulse {
          0%, 100% { opacity: 1; }
          50% { opacity: 0.4; }
        }
        @keyframes hmi-flow {
          0% { offset-distance: 0%; opacity: 0; }
          10% { opacity: 0.9; }
          90% { opacity: 0.9; }
          100% { offset-distance: 100%; opacity: 0; }
        }
        @keyframes hmi-flow-dot {
          0% { opacity: 0; }
          15% { opacity: 0.8; }
          85% { opacity: 0.8; }
          100% { opacity: 0; }
        }
      `}</style>
    </div>
  );
}

// ── Sub-Components ───────────────────────────────────────────────

function MeasurementBox({
  x,
  y,
  label,
  value,
  unit,
  live,
  warning,
  danger,
}: {
  x: number;
  y: number;
  label: string;
  value: string;
  unit: string;
  live: boolean;
  warning?: boolean;
  danger?: boolean;
}) {
  const color = danger ? C.alarm : warning ? C.warning : live ? C.measurement : C.textMuted;
  return (
    <g>
      <rect
        x={x}
        y={y}
        width={70}
        height={30}
        rx="3"
        fill="rgba(15,23,42,0.85)"
        stroke={color}
        strokeWidth="1"
        opacity="0.9"
      />
      <text x={x + 5} y={y + 11} fill={C.textDim} fontSize="7" fontWeight="bold" letterSpacing="1">
        {label}
      </text>
      <text
        x={x + 5}
        y={y + 24}
        fill={color}
        fontSize="12"
        fontWeight="bold"
        filter={danger ? "url(#glow-red)" : warning ? "url(#glow-amber)" : live ? "url(#glow-amber)" : undefined}
      >
        {value}
        <tspan fontSize="8" fill={C.textDim}> {unit}</tspan>
      </text>
    </g>
  );
}

function BarGauge({
  x,
  y,
  width,
  label,
  value,
  min,
  max,
  unit,
  nominal,
  warnLow,
  warnHigh,
}: {
  x: number;
  y: number;
  width: number;
  label: string;
  value: number;
  min: number;
  max: number;
  unit: string;
  nominal?: number;
  warnLow?: number;
  warnHigh?: number;
}) {
  const pct = Math.max(0, Math.min(1, (value - min) / (max - min)));
  const barWidth = width - 80;
  const isWarn =
    (warnLow !== undefined && value < warnLow) ||
    (warnHigh !== undefined && value > warnHigh);
  const barColor = value === 0 ? C.deenergized : isWarn ? C.warning : C.ok;

  return (
    <g>
      <text x={x} y={y + 10} fill={C.textDim} fontSize="8" fontWeight="bold" letterSpacing="1">
        {label}
      </text>
      {/* Track */}
      <rect x={x} y={y + 16} width={barWidth} height={8} rx="2" fill="rgba(30,41,59,0.8)" />
      {/* Fill */}
      <rect
        x={x}
        y={y + 16}
        width={Math.max(2, barWidth * pct)}
        height={8}
        rx="2"
        fill={barColor}
        opacity={0.8}
      />
      {/* Nominal marker */}
      {nominal !== undefined && (
        <line
          x1={x + barWidth * ((nominal - min) / (max - min))}
          y1={y + 14}
          x2={x + barWidth * ((nominal - min) / (max - min))}
          y2={y + 26}
          stroke={C.textDim}
          strokeWidth="1"
          strokeDasharray="2 2"
        />
      )}
      {/* Value */}
      <text
        x={x + barWidth + 8}
        y={y + 23}
        fill={isWarn ? C.warning : C.text}
        fontSize="11"
        fontWeight="bold"
      >
        {value.toFixed(value < 10 ? 1 : 0)}
        <tspan fill={C.textDim} fontSize="8"> {unit}</tspan>
      </text>
    </g>
  );
}

function FlowParticles({
  x1,
  y1,
  x2,
  y2,
  vertical,
}: {
  x1: number;
  y1: number;
  x2: number;
  y2: number;
  vertical?: boolean;
}) {
  // Render 3 animated dots along the path
  const count = 3;
  return (
    <g>
      {Array.from({ length: count }).map((_, i) => {
        const dur = 1.5;
        const delay = (i / count) * dur;
        return (
          <circle key={i} r="2.5" fill={C.energized} opacity="0">
            <animate
              attributeName={vertical ? "cy" : "cx"}
              from={vertical ? y1 : x1}
              to={vertical ? y2 : x2}
              dur={`${dur}s`}
              begin={`${delay}s`}
              repeatCount="indefinite"
            />
            <animate
              attributeName={vertical ? "cx" : "cy"}
              values={String(vertical ? x1 : y1)}
              dur={`${dur}s`}
              begin={`${delay}s`}
              repeatCount="indefinite"
            />
            <animate
              attributeName="opacity"
              values="0;0.8;0.8;0"
              keyTimes="0;0.1;0.85;1"
              dur={`${dur}s`}
              begin={`${delay}s`}
              repeatCount="indefinite"
            />
          </circle>
        );
      })}
    </g>
  );
}

function StatusIndicator({ label, ok }: { label: string; ok: boolean }) {
  return (
    <div className="flex items-center gap-1.5">
      <div
        className="h-2 w-2 rounded-full"
        style={{
          background: ok ? C.ok : C.alarm,
          boxShadow: ok ? `0 0 4px ${C.ok}` : `0 0 4px ${C.alarm}`,
        }}
      />
      <span className="text-[9px] font-bold tracking-wider" style={{ color: ok ? C.textDim : C.alarm }}>
        {label}
      </span>
    </div>
  );
}

function DeviceControlPopup({
  popup,
  relay,
  recloser,
  regulator,
  execCmd,
}: {
  popup: ControlPopup;
  relay?: Record<string, number | boolean | string>;
  recloser?: Record<string, number | boolean | string>;
  regulator?: Record<string, number | boolean | string>;
  execCmd: (device: string, command: string, value?: number) => void;
}) {
  const { device, x, y } = popup;

  // Position the popup to the left of the device to avoid SVG overflow
  const px = Math.min(x - 180, 800);
  const py = Math.max(y - 40, 50);

  const buttons: { label: string; cmd: string; variant: "danger" | "ok" | "warn" | "default" }[] = [];

  if (device === "relay") {
    buttons.push(
      { label: "TRIP", cmd: "trip", variant: "danger" },
      { label: "CLOSE", cmd: "close", variant: "ok" },
      { label: "LOCKOUT", cmd: "lockout", variant: "warn" },
      { label: "UNLOCK", cmd: "unlock", variant: "default" },
      { label: "INJECT FAULT", cmd: "inject_fault", variant: "danger" },
      { label: "CLEAR FAULT", cmd: "clear_fault", variant: "default" },
    );
  } else if (device === "recloser") {
    buttons.push(
      { label: "OPEN", cmd: "open", variant: "danger" },
      { label: "CLOSE", cmd: "close", variant: "ok" },
      { label: "ENABLE AR", cmd: "enable_reclose", variant: "ok" },
      { label: "DISABLE AR", cmd: "disable_reclose", variant: "warn" },
      { label: "RESET LO", cmd: "reset_lockout", variant: "default" },
      { label: "INJECT FAULT", cmd: "inject_fault", variant: "danger" },
      { label: "CLEAR FAULT", cmd: "clear_fault", variant: "default" },
    );
  } else if (device === "regulator") {
    buttons.push(
      { label: "RAISE TAP", cmd: "raise_tap", variant: "default" },
      { label: "LOWER TAP", cmd: "lower_tap", variant: "default" },
      { label: "MANUAL", cmd: "set_manual", variant: "warn" },
      { label: "AUTO", cmd: "set_auto", variant: "ok" },
    );
  }

  const variantColors: Record<string, { bg: string; border: string; text: string; hover: string }> = {
    danger: { bg: "rgba(239,68,68,0.1)", border: "rgba(239,68,68,0.4)", text: C.alarm, hover: "rgba(239,68,68,0.25)" },
    ok: { bg: "rgba(34,197,94,0.1)", border: "rgba(34,197,94,0.4)", text: C.ok, hover: "rgba(34,197,94,0.25)" },
    warn: { bg: "rgba(245,158,11,0.1)", border: "rgba(245,158,11,0.4)", text: C.warning, hover: "rgba(245,158,11,0.25)" },
    default: { bg: "rgba(100,116,139,0.1)", border: "rgba(100,116,139,0.4)", text: C.text, hover: "rgba(100,116,139,0.25)" },
  };

  const panelWidth = 160;
  const buttonHeight = 24;
  const padding = 8;
  const headerHeight = 28;
  const panelHeight = headerHeight + padding + buttons.length * (buttonHeight + 4) + padding;

  return (
    <g onClick={(e) => e.stopPropagation()}>
      {/* Backdrop shadow */}
      <rect
        x={px - 2}
        y={py - 2}
        width={panelWidth + 4}
        height={panelHeight + 4}
        rx="6"
        fill="rgba(0,0,0,0.5)"
      />
      {/* Panel */}
      <rect
        x={px}
        y={py}
        width={panelWidth}
        height={panelHeight}
        rx="4"
        fill="rgba(15,23,42,0.95)"
        stroke={C.energized}
        strokeWidth="1"
      />
      {/* Title */}
      <text
        x={px + 10}
        y={py + 18}
        fill={C.energized}
        fontSize="10"
        fontWeight="bold"
        letterSpacing="1"
      >
        {device.toUpperCase()} CONTROL
      </text>

      {/* Buttons */}
      {buttons.map((btn, i) => {
        const by = py + headerHeight + padding + i * (buttonHeight + 4);
        const vc = variantColors[btn.variant];
        return (
          <g
            key={btn.cmd}
            className="cursor-pointer"
            onClick={() => execCmd(device, btn.cmd)}
          >
            <rect
              x={px + 8}
              y={by}
              width={panelWidth - 16}
              height={buttonHeight}
              rx="3"
              fill={vc.bg}
              stroke={vc.border}
              strokeWidth="1"
              onMouseOver={(e) => e.currentTarget.setAttribute("fill", vc.hover)}
              onMouseOut={(e) => e.currentTarget.setAttribute("fill", vc.bg)}
            />
            <text
              x={px + panelWidth / 2}
              y={by + 16}
              textAnchor="middle"
              fill={vc.text}
              fontSize="9"
              fontWeight="bold"
              letterSpacing="0.5"
              className="pointer-events-none"
            >
              {btn.label}
            </text>
          </g>
        );
      })}
    </g>
  );
}
