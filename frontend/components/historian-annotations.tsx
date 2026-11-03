"use client";

import { useEffect, useId, useRef, useState, type CSSProperties } from "react";
import { createPortal } from "react-dom";
import { historyBreaks, type History, type HistorianEvent } from "../lib/historian-api";
import { formatPointValue, formatUtcTime, pointLabel, STALE_REASON_TEXT } from "../lib/operator-format";

const eventTime = (at: string) => formatUtcTime(at).slice(0, 8);

export function eventDescription(event: HistorianEvent): string {
  // Events normalise settled steps to their numeric position.
  const value = (v: HistorianEvent["from"]) => event.point === "oltc-1-tap" && typeof v === "number" ? String(v) : formatPointValue(event.point, v);
  return `${event.point === "oltc-1-tap" ? "T1 tap" : pointLabel(event.point)} ${value(event.from)} → ${value(event.to)}`;
}

export function breakDescriptions(history: History, events: HistorianEvent[]) {
  return historyBreaks(history).map((interval) => {
    const nearby = events.filter((event) => {
      const at = Date.parse(event.at);
      return at >= Date.parse(interval.from) - 2000 && at <= Date.parse(interval.to) + 2000;
    });
    const reasons = interval.reasons.map((reason) => reason === "measurement_invalid" ? "Measurement invalid" : STALE_REASON_TEXT[reason as keyof typeof STALE_REASON_TEXT] ?? reason);
    return {
      ...interval,
      text: `${reasons.join("; ")} ${eventTime(interval.from)}–${eventTime(interval.to)} UTC${nearby.length ? `, around ${nearby.map(eventDescription).join("; ")}` : ""}`,
    };
  });
}

function Annotation({ label, description = label, left, width, event }: { label: string; description?: string; left: number; width?: number; event?: boolean }) {
  const id = useId();
  const trigger = useRef<HTMLButtonElement>(null);
  const tooltip = useRef<HTMLDivElement>(null);
  const hideTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const [mode, setMode] = useState<"hover" | "focus" | "pinned" | null>(null);
  const [position, setPosition] = useState<CSSProperties>({});
  const cancelHide = () => clearTimeout(hideTimer.current);
  const show = (next: Exclude<typeof mode, null>) => {
    cancelHide();
    const rect = trigger.current!.getBoundingClientRect();
    const tooltipWidth = Math.min(320, window.innerWidth - 32);
    const above = rect.top > window.innerHeight / 2;
    setPosition({
      width: tooltipWidth,
      left: Math.max(16, Math.min(rect.left + rect.width / 2 - tooltipWidth / 2, window.innerWidth - tooltipWidth - 16)),
      ...(above ? { bottom: window.innerHeight - rect.top + 6 } : { top: rect.bottom + 6 }),
      maxHeight: Math.max(32, (above ? rect.top : window.innerHeight - rect.bottom) - 22),
    });
    setMode((current) => current === "pinned" || (current === "focus" && next === "hover") ? current : next);
  };
  const queueHide = () => {
    cancelHide();
    // Let the pointer travel from the trigger into the tooltip.
    if (mode === "hover") hideTimer.current = setTimeout(() => setMode((current) => current === "hover" ? null : current), 150);
  };
  useEffect(() => {
    if (!mode) return;
    const outside = (event: PointerEvent) => {
      if (event.target instanceof Node && !trigger.current?.contains(event.target) && !tooltip.current?.contains(event.target)) setMode(null);
    };
    const escape = (event: KeyboardEvent) => { if (event.key === "Escape") setMode(null); };
    const reposition = (event: Event) => {
      if (!(event.target instanceof Node) || !tooltip.current?.contains(event.target)) setMode(null);
    };
    document.addEventListener("pointerdown", outside);
    document.addEventListener("keydown", escape);
    window.addEventListener("resize", reposition);
    window.addEventListener("scroll", reposition, true);
    return () => {
      clearTimeout(hideTimer.current);
      document.removeEventListener("pointerdown", outside);
      document.removeEventListener("keydown", escape);
      window.removeEventListener("resize", reposition);
      window.removeEventListener("scroll", reposition, true);
    };
  }, [mode]);
  return <>
    {event ? <span aria-hidden="true" data-event-guide style={{ left: `${left}%` }}
      className="pointer-events-none absolute top-0 bottom-0 w-px bg-slate-300/70" /> :
      <span aria-hidden="true" data-break-band style={{ left: `${left}%`, width: `${width}%`, minWidth: "2px" }}
        className="pointer-events-none absolute bottom-0 h-[calc(100%-2rem)] -translate-x-1/2 bg-amber-300/20" />}
    {/* A native button owns click/focus instead of a hover-only primitive
        whose pointerdown/click handlers can dismiss a touch activation. */}
    <button ref={trigger} type="button" aria-label={label} aria-describedby={mode ? id : undefined}
      onClick={() => mode === "pinned" ? setMode(null) : show("pinned")}
      onFocus={() => show("focus")} onBlur={() => setMode(null)}
      onPointerMove={(event) => { cancelHide(); if (event.pointerType === "mouse" && !mode) show("hover"); }} onPointerLeave={queueHide}
      style={{ left: event ? `clamp(12px, ${left}%, calc(100% - 12px))` : `${left}%`, ...(width === undefined ? {} : { width: `${width}%` }) }}
      className={`pointer-events-auto absolute flex min-w-6 -translate-x-1/2 items-center justify-center rounded-sm border-0 bg-transparent p-0 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-sky-300 ${event ? "top-0 h-7 w-7" : "bottom-0 h-[calc(100%-2rem)]"}`}>
      {event && <span aria-hidden="true" className="size-2.5 rounded-full border border-slate-200 bg-slate-400" />}
    </button>
    {mode && createPortal(<div ref={tooltip} id={id} role="tooltip" style={position}
      onPointerEnter={cancelHide} onPointerLeave={queueHide}
      className="fixed z-50 overflow-auto rounded-md border border-slate-700 bg-slate-900 px-2.5 py-1 text-[11px] font-medium whitespace-pre-line text-slate-200 shadow-lg wrap-anywhere">{description}</div>, document.body)}
  </>;
}

export function groupHistorianEvents(events: HistorianEvent[]) {
  // Tolerance: 0 ms (one exact ingest instant). Do not merge nearby but
  // unrelated operations without measured evidence of snapshot spacing.
  const simultaneous = new Map<number, HistorianEvent[]>();
  for (const event of events) {
    const at = Date.parse(event.at);
    const group = simultaneous.get(at) ?? [];
    group.push(event);
    simultaneous.set(at, group);
  }
  return [...simultaneous].sort(([a], [b]) => a - b).map(([at, events]) => ({
    at: new Date(at).toISOString(), events,
    protection: events.some((event) => event.to === true && /(?:protection-trip|lockout|fault-passage)$/.test(event.point)),
  }));
}

function eventSummary(event: HistorianEvent) {
  const device = event.point.startsWith("feeder-") ? "CB1" : event.point.startsWith("f2-") ? "F2" : event.point.startsWith("rmu-1-") ? "RMU1" : "RMU2";
  if (["feeder-breaker-position", "f2-position", "rmu-1-position", "rmu-2-position"].includes(event.point)) return `${device} ${formatPointValue(event.point, event.to)}`;
  if (event.point.endsWith("protection-trip")) return `${device} ${event.to ? "TRIPPED" : "PROTECTION RESET"}`;
  if (event.point.endsWith("lockout")) return `${device} ${event.to ? "LOCKED OUT" : "LOCKOUT RESET"}`;
  if (event.point.endsWith("fault-passage")) return `${device} ${event.to ? "FAULT PASSED" : "FPI RESET"}`;
  return eventDescription(event);
}

export function HistorianAnnotations({ history, events, xmin, xmax, showEvents = true }: { history?: History; events: HistorianEvent[]; xmin: number; xmax: number; showEvents?: boolean }) {
  const percent = (at: string) => Math.max(0, Math.min(100, (Date.parse(at) - xmin) / (xmax - xmin || 1) * 100));
  const groups = groupHistorianEvents(events.filter((event) => Date.parse(event.at) >= xmin && Date.parse(event.at) <= xmax));
  return <div className="pointer-events-none absolute inset-0" aria-label="Trend annotations">
    {history && breakDescriptions(history, events).map((interval, i) => <Annotation key={`break-${i}`} label={interval.text}
      left={(percent(interval.from) + percent(interval.to)) / 2} width={percent(interval.to) - percent(interval.from)} />)}
    {showEvents && groups.map((group) => {
      const description = `${eventTime(group.at)} UTC ${group.events.map(eventDescription).join("; ")}`;
      return <Annotation key={group.at} event left={percent(group.at)} description={description} label={description} />;
    })}
  </div>;
}

export function HistorianEventList({ history, events }: { history: History; events: HistorianEvent[] }) {
  const breaks = breakDescriptions(history, events);
  const groups = groupHistorianEvents(events).reverse();
  return <div className="min-w-0 space-y-2 text-xs text-slate-300 wrap-anywhere">
    <h3 className="font-semibold">Events in this window</h3>
    {groups.length ? <ul aria-label="Archived events" tabIndex={0} className="max-h-60 space-y-1 overflow-y-auto">{groups.map((group) =>
      <li key={group.at}><time dateTime={group.at}>{eventTime(group.at)} UTC</time>{group.protection ? " · protection" : ""} · {group.events.map(eventSummary).join(" · ")}</li>)}</ul> : <p>No major status changes observed.</p>}
    {breaks.length > 0 && <details><summary className="cursor-pointer text-amber-300">Why the line breaks ({breaks.length})</summary>
      <ul className="mt-2 space-y-1">{breaks.map((interval, i) => <li key={i}>{interval.text}</li>)}</ul>
    </details>}
  </div>;
}
