"use client";

import { useState } from "react";
import { historyBreaks, type History, type HistorianEvent } from "../lib/historian-api";
import { formatPointValue, formatUtcTime, pointLabel, STALE_REASON_TEXT } from "../lib/operator-format";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "./ui/tooltip";

export function eventDescription(event: HistorianEvent): string {
  // Events normalise settled steps to their numeric position.
  const value = (v: HistorianEvent["from"]) => event.point === "oltc-1-tap" && typeof v === "number" ? String(v) : formatPointValue(event.point, v);
  return `${pointLabel(event.point)} ${value(event.from)} → ${value(event.to)}`;
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
      text: `${reasons.join("; ")} ${formatUtcTime(interval.from)}–${formatUtcTime(interval.to)} UTC (observed interval)${nearby.length ? `; near ${nearby.map(eventDescription).join("; ")} (time correlation, not proven cause)` : ""}`,
    };
  });
}

function Annotation({ label, description = label, left, width, event, warning }: { label: string; description?: string; left: number; width?: number; event?: boolean; warning?: boolean }) {
  const [open, setOpen] = useState(false);
  return <Tooltip open={open} onOpenChange={setOpen}>
    <TooltipTrigger asChild>
      <button type="button" aria-label={label} onClick={(event) => { event.preventDefault(); setOpen(true); }}
        style={{ left: `${left}%`, ...(width === undefined ? {} : { width: `${width}%` }) }}
        className={`pointer-events-auto absolute flex min-w-6 items-center justify-center rounded-sm focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-sky-300 ${event ? "top-0 h-7 w-7 -translate-x-1/2" : "bottom-0 h-[calc(100%-2rem)] -translate-x-1/2 border-x border-dashed border-amber-300/50 bg-amber-300/5"}`}>
        {event && <span aria-hidden="true" className={`size-2.5 rounded-full border ${warning ? "border-rose-300 bg-rose-400" : "border-slate-200 bg-slate-400"}`} />}
      </button>
    </TooltipTrigger>
    <TooltipContent style={{ animation: "none" }} className="max-w-[min(24rem,calc(100vw-2rem))] whitespace-pre-line wrap-anywhere">{description}</TooltipContent>
  </Tooltip>;
}

export function HistorianAnnotations({ history, events, xmin, xmax }: { history: History; events: HistorianEvent[]; xmin: number; xmax: number }) {
  const percent = (at: string) => Math.max(0, Math.min(100, (Date.parse(at) - xmin) / (xmax - xmin || 1) * 100));
  // Several status changes can arrive in one snapshot. The topmost dot's
  // tooltip must not conceal the other facts at that same instant.
  const simultaneous = new Map<string, string>();
  for (const event of events) {
    const description = `${formatUtcTime(event.at)} UTC ${eventDescription(event)}`;
    const previous = simultaneous.get(event.at);
    simultaneous.set(event.at, previous ? `${previous}\n${description}` : description);
  }
  return <TooltipProvider delayDuration={0}>
    <div className="pointer-events-none absolute inset-0" aria-label="Trend annotations">
      {breakDescriptions(history, events).map((interval, i) => <Annotation key={`break-${i}`} label={interval.text}
        left={(percent(interval.from) + percent(interval.to)) / 2} width={percent(interval.to) - percent(interval.from)} />)}
      {events.map((event, i) => <Annotation key={`event-${i}`} event left={percent(event.at)}
        warning={event.to === "open" || (event.to === true && /trip|earth|fault-passage/.test(event.point))}
        description={simultaneous.get(event.at)}
        label={`${formatUtcTime(event.at)} UTC ${eventDescription(event)}`} />)}
    </div>
  </TooltipProvider>;
}

export function HistorianEventList({ history, events }: { history: History; events: HistorianEvent[] }) {
  const breaks = breakDescriptions(history, events);
  return <div className="min-w-0 space-y-2 text-xs text-slate-300 wrap-anywhere">
    <h3 className="font-semibold">Events in this window</h3>
    {events.length ? <ul className="space-y-1">{[...events].sort((a, b) => Date.parse(b.at) - Date.parse(a.at)).slice(0, 10).map((event, i) =>
      <li key={i}><time dateTime={event.at}>{formatUtcTime(event.at)} UTC</time> {eventDescription(event)}</li>)}</ul> : <p>No major status changes observed.</p>}
    {events.length > 10 && <p className="text-slate-400">Newest 10 of {events.length} events. All markers remain on the chart.</p>}
    {breaks.length > 0 && <details><summary className="cursor-pointer text-amber-300">Why the line breaks ({breaks.length})</summary>
      <ul className="mt-2 space-y-1">{breaks.map((interval, i) => <li key={i}>{interval.text}</li>)}</ul>
    </details>}
  </div>;
}
