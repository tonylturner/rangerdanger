"use client";

import { useSyncExternalStore, useCallback } from "react";
import { useCurriculumScope } from "./curriculum-scope";
import {
  FIREWALL_TRACK,
  curriculumKey,
  readCurriculum,
  removeCurriculum,
  writeCurriculum,
  type CurriculumScope,
} from "./curriculum-storage";

// useFirewallTrack — student's choice of how to interact with containd
// across the firewall implementation labs (2.2 / 2.3 / 2.3-bonus / 2.4).
//
// guided   — DEFAULT. Student applies policies via side-panel buttons
//            (Apply Hardened, Apply Your Plan). Still walks the containd
//            interface for understanding, just doesn't author rules
//            themselves. This is the default workshop path so most
//            students never touch containd's commit flow.
// technical — the Advanced opt-in. Student authors and commits the
//            policy directly in containd's web UI or CLI. Side-panel
//            buttons stay as a fallback. Tracked by the banner ("Your
//            custom policy") once a commit lands.
// null     — vestigial. Kept in the type for back-compat, but readTrack
//            now falls back to "guided", so the firewall labs default to
//            the Guided path instead of force-picking.

export type FirewallTrack = "guided" | "technical" | null;

const TRACK_CHANGED_EVENT = "rangerdanger.firewall-track-changed";

function readTrack(scope: CurriculumScope): FirewallTrack {
  const v = readCurriculum(scope, FIREWALL_TRACK);
  if (v === "guided" || v === "technical") return v;
  return "guided";
}

export function useFirewallTrack(): {
  track: FirewallTrack;
  setTrack: (t: FirewallTrack) => void;
} {
  // The server/hydration snapshot stays guided. React checks the scoped
  // browser snapshot on mount and when either storage event fires.
  const scope = useCurriculumScope();
  const subscribe = useCallback((onChange: () => void) => {
    const key = curriculumKey(scope, FIREWALL_TRACK);
    const onStorage = (e: StorageEvent) => {
      if (e.key === key) onChange();
    };
    window.addEventListener("storage", onStorage);
    window.addEventListener(TRACK_CHANGED_EVENT, onChange);
    return () => {
      window.removeEventListener("storage", onStorage);
      window.removeEventListener(TRACK_CHANGED_EVENT, onChange);
    };
  }, [scope]);
  const track = useSyncExternalStore(subscribe, () => readTrack(scope), () => "guided" as const);

  const setTrack = useCallback((t: FirewallTrack) => {
    if (typeof window === "undefined") return;
    if (t === null) removeCurriculum(scope, FIREWALL_TRACK);
    else writeCurriculum(scope, FIREWALL_TRACK, t);
    // Custom event so other components in the same tab pick up the
    // change immediately (the storage event only fires cross-tab).
    window.dispatchEvent(new Event(TRACK_CHANGED_EVENT));
  }, [scope]);

  return { track, setTrack };
}
