"use client";

import { useFirewallTrack, type FirewallTrack } from "../lib/use-firewall-track";
import { Wrench, Hand, Check } from "lucide-react";

// TrackPicker — renders inline in Lab 2.2 step 1 (via the
// :::track-picker directive). The student's choice persists to
// localStorage and is read by later steps and labs to vary the
// "apply policy" / "interact with containd" sections, plus the
// side-panel buttons in scenario-runner.tsx.
//
// Force-pick is enforced separately in scenario-runner.tsx by
// gating the Next button on track !== null when scenario is
// firewall-implementation + step index 0.

type Card = {
  value: Exclude<FirewallTrack, null>;
  title: string;
  oneLiner: string;
  detail: string;
  accent: string;
  borderActive: string;
  borderIdle: string;
  bgActive: string;
  Icon: typeof Wrench;
};

const CARDS: Card[] = [
  {
    value: "guided",
    title: "Guided (default)",
    oneLiner: "Click to apply, explore the interfaces",
    detail:
      "You apply policies with the side-panel buttons (Apply Hardened, Apply Your Plan) and walk containd's web UI / CLI to understand what landed. No rule authoring and no commit step. This is the default workshop path — recommended for everyone focusing on the segmentation lesson rather than firewall policy syntax.",
    accent: "text-emerald-300",
    borderActive: "border-emerald-500",
    borderIdle: "border-slate-700",
    bgActive: "bg-emerald-950/40",
    Icon: Hand,
  },
  {
    value: "technical",
    title: "Advanced",
    oneLiner: "Author and commit the policy yourself",
    detail:
      "Opt in to write the rules in containd's web UI or CLI (your choice) and commit them yourself. The banner detects your commit and labels it 'Your custom policy'. The side-panel buttons stay visible as a fallback. For engineers comfortable with firewall policy syntax who want the hands-on rep.",
    accent: "text-sky-300",
    borderActive: "border-sky-500",
    borderIdle: "border-slate-700",
    bgActive: "bg-sky-950/40",
    Icon: Wrench,
  },
];

export function TrackPicker() {
  const { track, setTrack } = useFirewallTrack();

  return (
    <div className="my-4 rounded-xl border border-slate-700 bg-slate-900/50 p-4">
      <div className="mb-1 text-[11px] font-bold uppercase tracking-wider text-slate-400">
        How do you want to work?
      </div>
      <div className="mb-3 text-xs text-slate-400">
        <span className="text-emerald-300">Guided</span> is selected by
        default — just continue. Switch to Advanced if you&apos;d rather
        author the rules yourself; you can change this from the side
        panel on any firewall lab anytime.
      </div>
      <div className="grid gap-3 sm:grid-cols-2">
        {CARDS.map((card) => {
          const active = track === card.value;
          const Icon = card.Icon;
          return (
            <button
              key={card.value}
              type="button"
              onClick={() => setTrack(card.value)}
              className={`relative rounded-lg border-2 p-3 text-left transition-colors ${
                active
                  ? `${card.borderActive} ${card.bgActive}`
                  : `${card.borderIdle} bg-slate-950/40 hover:border-slate-500`
              }`}
            >
              {active && (
                <Check className="absolute right-2 top-2 h-4 w-4 text-emerald-400" />
              )}
              <div className="mb-1 flex items-center gap-2">
                <Icon className={`h-4 w-4 ${card.accent}`} />
                <span className={`text-sm font-bold ${card.accent}`}>
                  {card.title}
                </span>
              </div>
              <div className="mb-2 text-xs font-medium text-slate-200">
                {card.oneLiner}
              </div>
              <div className="text-[11px] leading-relaxed text-slate-400">
                {card.detail}
              </div>
            </button>
          );
        })}
      </div>
    </div>
  );
}
