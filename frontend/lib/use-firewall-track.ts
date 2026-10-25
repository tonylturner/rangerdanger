"use client";

import { useEffect, useState, useCallback } from "react";
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
  // Default to "guided" (not null) so the firewall labs open on the
  // Guided path with no force-pick and no first-render flash of the
  // technical block. A returning student's saved choice is restored in
  // the effect below.
  const scope = useCurriculumScope();
  const [track, setTrackState] = useState<FirewallTrack>("guided");

  useEffect(() => {
    setTrackState(readTrack(scope));
    const key = curriculumKey(scope, FIREWALL_TRACK);
    const onStorage = (e: StorageEvent) => {
      if (e.key === key) setTrackState(readTrack(scope));
    };
    const onCustom = () => setTrackState(readTrack(scope));
    window.addEventListener("storage", onStorage);
    window.addEventListener(TRACK_CHANGED_EVENT, onCustom);
    return () => {
      window.removeEventListener("storage", onStorage);
      window.removeEventListener(TRACK_CHANGED_EVENT, onCustom);
    };
  }, [scope]);

  const setTrack = useCallback((t: FirewallTrack) => {
    if (typeof window === "undefined") return;
    if (t === null) removeCurriculum(scope, FIREWALL_TRACK);
    else writeCurriculum(scope, FIREWALL_TRACK, t);
    setTrackState(t);
    // Custom event so other components in the same tab pick up the
    // change immediately (the storage event only fires cross-tab).
    window.dispatchEvent(new Event(TRACK_CHANGED_EVENT));
  }, [scope]);

  return { track, setTrack };
}
