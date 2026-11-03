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

function Annotation({ label, description = label, left, width, event, warning }: { label: string; description?: string; left: number; width?: number; event?: boolean; warning?: boolean }) {
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
      className={`pointer-events-none absolute top-3.5 bottom-0 w-px ${warning ? "bg-rose-300/25" : "bg-slate-400/25"}`} /> :
      <span aria-hidden="true" data-break-band style={{ left: `${left}%`, width: `${width}%`, minWidth: "2px" }}
        className="pointer-events-none absolute bottom-0 h-[calc(100%-2rem)] -translate-x-1/2 bg-amber-300/20" />}
    {/* A native button owns click/focus instead of a hover-only primitive
        whose pointerdown/click handlers can dismiss a touch activation. */}
    <button ref={trigger} type="button" aria-label={label} aria-describedby={mode ? id : undefined}
      onClick={() => mode === "pinned" ? setMode(null) : show("pinned")}
      onFocus={() => show("focus")} onBlur={() => setMode(null)}
      onPointerMove={(event) => { cancelHide(); if (event.pointerType === "mouse" && !mode) show("hover"); }} onPointerLeave={queueHide}
      style={{ left: `${left}%`, ...(width === undefined ? {} : { width: `${width}%` }) }}
      className={`pointer-events-auto absolute flex min-w-6 -translate-x-1/2 items-center justify-center rounded-sm border-0 bg-transparent p-0 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-sky-300 ${event ? "top-0 h-7 w-7" : "bottom-0 h-[calc(100%-2rem)]"}`}>
      {event && <span aria-hidden="true" className={`size-2.5 rounded-full border ${warning ? "border-rose-300 bg-rose-400" : "border-slate-200 bg-slate-400"}`} />}
    </button>
    {mode && createPortal(<div ref={tooltip} id={id} role="tooltip" style={position}
      onPointerEnter={cancelHide} onPointerLeave={queueHide}
      className="fixed z-50 overflow-auto rounded-md border border-slate-700 bg-slate-900 px-2.5 py-1 text-[11px] font-medium whitespace-pre-line text-slate-200 shadow-lg wrap-anywhere">{description}</div>, document.body)}
  </>;
}

export function HistorianAnnotations({ history, events, xmin, xmax }: { history: History; events: HistorianEvent[]; xmin: number; xmax: number }) {
  const percent = (at: string) => Math.max(0, Math.min(100, (Date.parse(at) - xmin) / (xmax - xmin || 1) * 100));
  // Several status changes can arrive in one snapshot. The topmost dot's
  // tooltip must not conceal the other facts at that same instant.
  const simultaneous = new Map<string, string>();
  for (const event of events) {
    const description = `${eventTime(event.at)} UTC ${eventDescription(event)}`;
    const previous = simultaneous.get(event.at);
    simultaneous.set(event.at, previous ? `${previous}\n${description}` : description);
  }
  return <div className="pointer-events-none absolute inset-0" aria-label="Trend annotations">
    {breakDescriptions(history, events).map((interval, i) => <Annotation key={`break-${i}`} label={interval.text}
      left={(percent(interval.from) + percent(interval.to)) / 2} width={percent(interval.to) - percent(interval.from)} />)}
    {events.map((event, i) => <Annotation key={`event-${i}`} event left={percent(event.at)}
      warning={event.to === "open" || (event.to === true && /trip|earth|fault-passage/.test(event.point))}
      description={simultaneous.get(event.at)}
      label={`${eventTime(event.at)} UTC ${eventDescription(event)}`} />)}
  </div>;
}

export function HistorianEventList({ history, events }: { history: History; events: HistorianEvent[] }) {
  const breaks = breakDescriptions(history, events);
  return <div className="min-w-0 space-y-2 text-xs text-slate-300 wrap-anywhere">
    <h3 className="font-semibold">Events in this window</h3>
    {events.length ? <ul className="space-y-1">{[...events].sort((a, b) => Date.parse(b.at) - Date.parse(a.at)).slice(0, 10).map((event, i) =>
      <li key={i}><time dateTime={event.at}>{eventTime(event.at)} UTC</time> {eventDescription(event)}</li>)}</ul> : <p>No major status changes observed.</p>}
    {events.length > 10 && <p className="text-slate-400">Newest 10 of {events.length} events. All markers remain on the chart.</p>}
    {breaks.length > 0 && <details><summary className="cursor-pointer text-amber-300">Why the line breaks ({breaks.length})</summary>
      <ul className="mt-2 space-y-1">{breaks.map((interval, i) => <li key={i}>{interval.text}</li>)}</ul>
    </details>}
  </div>;
}
