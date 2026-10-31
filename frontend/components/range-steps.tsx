import { Check, Loader2 } from "lucide-react";
import {
  TRANSITION_STEPS,
  transitionStepLabel,
  transitionStepNumber,
  type TransitionStep,
} from "../lib/range";

// The four transition steps with the current one spinning, earlier
// ones ticked and later ones dimmed.
export function RangeSteps({ step }: { step: TransitionStep }) {
  const current = transitionStepNumber(step);
  return (
    <ol className="space-y-1" aria-label={`Step ${current} of ${TRANSITION_STEPS.length}`}>
      {TRANSITION_STEPS.map((s, i) => {
        const n = i + 1;
        const state = n < current ? "done" : n === current ? "current" : "todo";
        return (
          <li
            key={s}
            aria-current={state === "current" ? "step" : undefined}
            className={`flex items-start gap-1.5 text-[11px] leading-snug ${
              state === "current" ? "text-sky-200" : state === "done" ? "text-slate-400" : "text-slate-600"
            }`}
          >
            <span className="mt-px flex h-3 w-3 shrink-0 items-center justify-center">
              {state === "done" && <Check className="h-3 w-3 text-emerald-400" />}
              {state === "current" && <Loader2 className="h-3 w-3 animate-spin text-sky-400" />}
              {state === "todo" && <span className="h-1.5 w-1.5 rounded-full bg-slate-700" />}
            </span>
            <span>{transitionStepLabel(s)}</span>
          </li>
        );
      })}
    </ol>
  );
}
