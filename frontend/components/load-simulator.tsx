"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { getSubstationState, sendLabControl } from "../lib/api";

// Load Simulator — a training-infrastructure control for exploring the OpenDSS
// physics engine. It drives the feeder loads (general/critical kW + PF) via the
// lab-control override; it is OFF until a learner engages it, so it never
// changes baseline RangerDanger behavior. Distinct from the SCADA controls
// below: it manipulates LOAD and grid state only — devices live on the
// Supervisory Control tab.
//
// Styling matches the app's slate/Inter/font-mono theme; the lime (#c6f24e)
// and amber (#f59e0b) accents are the deliberate "training infrastructure"
// markers (lime = brand, amber = the event preset).

const GEN_MAX_KW = 707;
const CRIT_MAX_KW = 300;
const LIME = "#c6f24e";

type Preset = { id: string; label: string; gen: number; crit: number; pf: number; event?: boolean };
const PRESETS: Preset[] = [
  { id: "overnight", label: "Overnight", gen: 25, crit: 45, pf: 0.98 },
  { id: "morning", label: "Morning ramp", gen: 55, crit: 60, pf: 0.95 },
  { id: "peak", label: "Peak load", gen: 85, crit: 75, pf: 0.92 },
  { id: "hotday", label: "Hot day", gen: 90, crit: 80, pf: 0.88 },
  { id: "loaddrop", label: "⚡ Large load drop", gen: 70, crit: 65, pf: 0.94, event: true },
];

const easeInOutQuad = (t: number) => (t < 0.5 ? 2 * t * t : 1 - Math.pow(-2 * t + 2, 2) / 2);
const clampPf = (p: number) => Math.max(80, Math.min(100, p));

export function LoadSimulator() {
  const [expanded, setExpanded] = useState(false);
  const [gen, setGen] = useState(0); // 0-100 %
  const [crit, setCrit] = useState(0); // 0-100 %
  const [pf, setPf] = useState(100); // 80-100 slider (=> 0.80-1.00)
  const [activePreset, setActivePreset] = useState<string | null>(null);
  const [engaged, setEngaged] = useState(false);
  const [tele, setTele] = useState<{ totalKw: number; pf: number } | null>(null);
  const [lastChange, setLastChange] = useState<{ label: string; ts: number } | null>(null);
  const [now, setNow] = useState(0);
  const [modal, setModal] = useState(false);

  // Refs mirror the animated slider values so ramps read live values (no stale
  // closures when one ramp chains into the next).
  const genRef = useRef(0);
  const critRef = useRef(0);
  const pfRef = useRef(100);
  const setGenV = (v: number) => { genRef.current = v; setGen(v); };
  const setCritV = (v: number) => { critRef.current = v; setCrit(v); };
  const setPfV = (v: number) => { pfRef.current = v; setPf(v); };

  const animRef = useRef<number | null>(null);
  const dropTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const pushRef = useRef<{ t: number; timer: ReturnType<typeof setTimeout> | null }>({ t: 0, timer: null });
  const initedRef = useRef(false);
  const engagedRef = useRef(false);

  // session-persistent expand/collapse
  useEffect(() => {
    if (sessionStorage.getItem("loadsim-expanded") === "1") setExpanded(true);
    setNow(Date.now());
  }, []);
  const toggleExpand = () =>
    setExpanded((e) => {
      sessionStorage.setItem("loadsim-expanded", e ? "0" : "1");
      return !e;
    });

  // live telemetry for the status string — every 500ms, even collapsed
  useEffect(() => {
    let live = true;
    const poll = async () => {
      try {
        const s = await getSubstationState();
        if (!live) return;
        const g = s.electrical.general_load_kw ?? 0;
        const c = s.electrical.critical_load_kw ?? 0;
        setTele({ totalKw: g + c, pf: s.electrical.power_factor ?? 0 });
        // seed sliders from the live (default) load once, before engaging
        if (!initedRef.current && !engagedRef.current) {
          initedRef.current = true;
          setGenV(Math.round((g / GEN_MAX_KW) * 100));
          setCritV(Math.round((c / CRIT_MAX_KW) * 100));
          setPfV(clampPf(Math.round((s.electrical.power_factor ?? 1) * 100)));
        }
      } catch {
        /* offline */
      }
    };
    poll();
    const id = setInterval(poll, 500);
    return () => {
      live = false;
      clearInterval(id);
    };
  }, []);

  // tick for the "Xs ago" audit line
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, []);

  // cancel any in-flight ramp / deferred timers on unmount (avoids state
  // updates and stray lab-control POSTs after the component is gone)
  useEffect(() => () => {
    if (animRef.current) cancelAnimationFrame(animRef.current);
    if (dropTimerRef.current) clearTimeout(dropTimerRef.current);
    if (pushRef.current.timer) clearTimeout(pushRef.current.timer);
  }, []);

  // Esc closes the reset modal
  useEffect(() => {
    if (!modal) return;
    const onKey = (e: KeyboardEvent) => { if (e.key === "Escape") setModal(false); };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [modal]);

  const engage = () => { engagedRef.current = true; setEngaged(true); };

  // throttled override push (active=true) so the 2s OpenDSS solve follows ramps
  const pushOverride = useCallback((g: number, c: number, p: number, force = false) => {
    const send = () => {
      pushRef.current.t = Date.now();
      sendLabControl({ active: true, general_load_pct: g, critical_load_pct: c, power_factor: p / 100 }).catch(() => {});
    };
    if (pushRef.current.timer) { clearTimeout(pushRef.current.timer); pushRef.current.timer = null; }
    const dt = Date.now() - pushRef.current.t;
    if (force || dt > 120) send();
    else pushRef.current.timer = setTimeout(send, 120 - dt);
  }, []);

  const audit = useCallback((command: string, target: string, detail: string) => {
    sendLabControl({ audit_command: command, audit_target: target, audit_detail: detail, source: "lab-control" }).catch(() => {});
  }, []);

  const cancelAnim = () => {
    if (animRef.current) cancelAnimationFrame(animRef.current);
    animRef.current = null;
    if (dropTimerRef.current) clearTimeout(dropTimerRef.current);
    dropTimerRef.current = null;
  };

  // ramp all three sliders to targets over durationMs, reading start from refs
  const ramp = useCallback((tg: number, tc: number, tp: number, durationMs: number, onDone?: () => void) => {
    cancelAnim();
    engage();
    const sg = genRef.current, sc = critRef.current, sp = pfRef.current;
    const start = performance.now();
    const stepFn = (ts: number) => {
      const t = Math.min(1, (ts - start) / durationMs);
      const e = easeInOutQuad(t);
      const g = sg + (tg - sg) * e, c = sc + (tc - sc) * e, p = sp + (tp - sp) * e;
      setGenV(g); setCritV(c); setPfV(p);
      pushOverride(g, c, p);
      if (t < 1) animRef.current = requestAnimationFrame(stepFn);
      else { animRef.current = null; setGenV(tg); setCritV(tc); setPfV(tp); pushOverride(tg, tc, tp, true); onDone?.(); }
    };
    animRef.current = requestAnimationFrame(stepFn);
  }, [pushOverride]);

  // ── presets ────────────────────────────────────────────────────
  const clickPreset = (preset: Preset) => {
    if (preset.event) { runLoadDrop(); return; }
    if (activePreset === preset.id) {
      // deselect: sliders stay, status returns to Steady state
      cancelAnim();
      setActivePreset(null);
      setLastChange({ label: "Steady state", ts: Date.now() });
      return;
    }
    setActivePreset(preset.id);
    ramp(preset.gen, preset.crit, preset.pf * 100, 3000);
    audit("set_preset", preset.id, preset.label);
    setLastChange({ label: `Preset: ${preset.label}`, ts: Date.now() });
  };

  // Large load drop — single-shot event, not a steady state.
  const runLoadDrop = () => {
    cancelAnim();
    setActivePreset("loaddrop");
    setLastChange({ label: "Large load drop", ts: Date.now() });
    audit("preset_start", "large_load_drop", "feeder heavily loaded with large customer");
    // 1. ramp to heavy load over 3s ...
    ramp(70, 65, 94, 3000, () => {
      // 2. pause ~400ms ...
      dropTimerRef.current = setTimeout(() => {
        const before = genRef.current;
        // 3. drop general load to 40% over 1.5s (data-center-style trip)
        ramp(40, critRef.current, pfRef.current, 1500, () => {
          const deltaKw = Math.round(((40 - before) / 100) * GEN_MAX_KW);
          audit("load_delta", "general", `${deltaKw} kW`);
          audit("preset_end", "new_steady_state", "load shed, new steady state");
          setLastChange({ label: "Load drop → new steady state", ts: Date.now() });
        });
      }, 400);
    });
  };

  // ── sliders ────────────────────────────────────────────────────
  const onSlider = (which: "gen" | "crit" | "pf", value: number) => {
    cancelAnim();
    setActivePreset(null); // touching a slider leaves any named state
    engage();
    let g = genRef.current, c = critRef.current, p = pfRef.current;
    if (which === "gen") { g = value; setGenV(value); }
    if (which === "crit") { c = value; setCritV(value); }
    if (which === "pf") { p = value; setPfV(value); }
    pushOverride(g, c, p);
  };
  const onSliderCommit = (which: "gen" | "crit" | "pf") => {
    if (which === "gen") audit("set_load", "general_load", `${Math.round((genRef.current / 100) * GEN_MAX_KW)} kW`);
    else if (which === "crit") audit("set_load", "critical_load", `${Math.round((critRef.current / 100) * CRIT_MAX_KW)} kW`);
    else audit("set_load", "power_factor", (pfRef.current / 100).toFixed(2));
    setLastChange({ label: `Manual ${which === "pf" ? "power factor" : which + " load"}`, ts: Date.now() });
  };

  // ── reset (load simulator only) ────────────────────────────────
  const doReset = () => {
    cancelAnim();
    setActivePreset(null);
    engagedRef.current = false;
    setEngaged(false);
    initedRef.current = false; // re-seed sliders from default telemetry next poll
    sendLabControl({
      active: false,
      source: "lab-control",
      audit_command: "reset_lab",
      audit_target: "baseline_restored",
      audit_detail: "load returned to default feeder",
    }).catch(() => {});
    setLastChange({ label: "Reset to baseline", ts: Date.now() });
    setModal(false);
  };

  // ── derived display ────────────────────────────────────────────
  const genKw = Math.round((gen / 100) * GEN_MAX_KW);
  const critKw = Math.round((crit / 100) * CRIT_MAX_KW);
  const statusKw = engaged ? genKw + critKw : Math.round(tele?.totalKw ?? 0);
  const statusPf = (tele?.pf ?? 0).toFixed(2);
  const stateName = activePreset ? PRESETS.find((p) => p.id === activePreset)?.label ?? "Steady state" : "Steady state";
  const agoSec = lastChange ? Math.max(0, Math.round((now - lastChange.ts) / 1000)) : 0;

  const presetClass = (p: Preset, active: boolean) => {
    if (p.event) return `border-amber-500 text-amber-500 font-semibold ${active ? "bg-amber-500/10" : ""}`;
    return active ? "border-[#c6f24e] bg-[#c6f24e]/10 text-[#c6f24e]" : "border-slate-700 text-slate-400 hover:bg-slate-800/40";
  };

  return (
    <div
      className="rounded-lg border border-slate-800 bg-slate-900/70"
      style={{ borderTop: "2px dashed #334155" }}
    >
      {/* Collapsed row — always visible */}
      <div className="flex items-center gap-3 px-4" style={{ minHeight: 50 }}>
        <span className="shrink-0 text-[11px] font-bold uppercase tracking-[0.12em] text-[#c6f24e]">
          Load Simulator
        </span>
        <span className="truncate font-mono text-xs text-slate-400">
          {stateName} &middot; {statusKw} kW &middot; PF {statusPf}
        </span>
        <div className="ml-auto flex shrink-0 items-center gap-2">
          <button
            onClick={() => setModal(true)}
            className="rounded border border-slate-700 px-2.5 py-1 text-[11px] text-slate-300 transition-colors hover:bg-slate-800/50"
          >
            Reset lab
          </button>
          <button
            onClick={toggleExpand}
            aria-label={expanded ? "Collapse" : "Expand"}
            className="rounded border border-slate-700 px-2 py-1 text-[11px] text-slate-400 transition-colors hover:bg-slate-800/50"
          >
            {expanded ? "▴" : "▾"}
          </button>
        </div>
      </div>

      {/* Expanded body */}
      {expanded && (
        <div className="px-4 pb-3">
          {/* GRID STATE */}
          <div className="mb-2 mt-1 flex items-center justify-between">
            <span className="text-[10px] font-medium uppercase tracking-wider text-slate-500">Grid State</span>
            <span className="font-mono text-[10px] text-slate-600">click again to deselect</span>
          </div>
          <div className="flex flex-wrap gap-2">
            {PRESETS.map((p) => {
              const active = activePreset === p.id;
              return (
                <button
                  key={p.id}
                  onClick={() => clickPreset(p)}
                  title={p.event ? "Simulates NERC Level 3 Alert scenario — sudden customer-initiated load reduction (May 2026)." : undefined}
                  className={`rounded-md border px-3 py-2 text-[12px] transition-colors ${presetClass(p, active)}`}
                >
                  {p.label}
                </button>
              );
            })}
          </div>

          {/* divider */}
          <div className="my-3 border-t border-slate-800" />

          {/* FINE TUNE */}
          <div className="mb-2 text-[10px] font-medium uppercase tracking-wider text-slate-500">Fine Tune</div>
          <div className="space-y-2">
            <SliderRow label="GENERAL LOAD" value={gen} readout={`${genKw} kW (${Math.round(gen)}%)`}
              onInput={(v) => onSlider("gen", v)} onCommit={() => onSliderCommit("gen")} />
            <SliderRow label="CRITICAL LOAD" value={crit} readout={`${critKw} kW (${Math.round(crit)}%)`}
              onInput={(v) => onSlider("crit", v)} onCommit={() => onSliderCommit("crit")} />
            <SliderRow label="POWER FACTOR" value={pf} min={80} max={100} readout={(pf / 100).toFixed(2)}
              onInput={(v) => onSlider("pf", v)} onCommit={() => onSliderCommit("pf")} />
          </div>

          {/* audit line */}
          <div className="mt-3 font-mono text-[11px] text-slate-500">
            Last change: <span className={lastChange ? "text-slate-400" : "text-slate-600"}>{lastChange ? lastChange.label : "— none —"}</span>
            {lastChange ? <> &middot; {agoSec}s ago</> : null} &middot; <span className="text-[#c6f24e]">lab-control</span>
          </div>
          <div className="mt-1 text-[10px] text-slate-600">
            Automatic regulator / cap-bank responses appear in the Command Audit tab tagged <span className="text-amber-500">auto</span>.
          </div>
        </div>
      )}

      {/* Reset modal — scoped to the Load Simulator only */}
      {modal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4" onClick={() => setModal(false)}>
          <div
            role="dialog" aria-modal="true" aria-label="Reset the Load Simulator"
            className="w-full max-w-md rounded-lg border border-slate-700 bg-slate-900 p-4"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="mb-2 text-sm font-bold text-slate-200">Reset the Load Simulator?</div>
            <div className="text-[12px] leading-relaxed text-slate-400">
              Returns the feeder load to its default (General 500 kW / Critical 200 kW) and clears any active preset. This resets{" "}
              <span className="text-[#c6f24e]">only the Load Simulator</span> — breakers, recloser, regulator, cap bank, faults, and
              lockouts are untouched (use Supervisory Control for those).
            </div>
            <div className="mt-2 text-[11px] text-slate-500">Current exercise progress is preserved.</div>
            <div className="mt-4 flex justify-end gap-2">
              <button onClick={() => setModal(false)}
                className="rounded border border-slate-700 px-3 py-1.5 text-[12px] text-slate-400 transition-colors hover:bg-slate-800/50">
                Cancel
              </button>
              <button onClick={doReset}
                className="rounded border border-[#c6f24e] bg-[#c6f24e] px-3 py-1.5 text-[12px] font-medium text-slate-900">
                Reset
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

function SliderRow({
  label, value, readout, onInput, onCommit, min = 0, max = 100,
}: {
  label: string; value: number; readout: string;
  onInput: (v: number) => void; onCommit: () => void; min?: number; max?: number;
}) {
  return (
    <div className="flex items-center gap-3">
      <span className="shrink-0 text-[11px] tracking-[0.04em] text-slate-400" style={{ width: 110 }}>{label}</span>
      <input
        type="range" min={min} max={max} step={1} value={Math.round(value)}
        aria-label={label} aria-valuetext={readout}
        onChange={(e) => onInput(Number(e.target.value))}
        onMouseUp={onCommit} onTouchEnd={onCommit}
        className="flex-1" style={{ accentColor: LIME }}
      />
      <span className="shrink-0 text-right font-mono text-xs font-bold text-slate-200" style={{ width: 90 }}>{readout}</span>
    </div>
  );
}
