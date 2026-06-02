"use client";

import { useEffect, useState, useCallback } from "react";

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

const STORAGE_KEY = "rangerdanger.firewall-track";

function readTrack(): FirewallTrack {
  if (typeof window === "undefined") return "guided";
  const v = window.localStorage.getItem(STORAGE_KEY);
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
  const [track, setTrackState] = useState<FirewallTrack>("guided");

  useEffect(() => {
    setTrackState(readTrack());
    const onStorage = (e: StorageEvent) => {
      if (e.key === STORAGE_KEY) setTrackState(readTrack());
    };
    const onCustom = () => setTrackState(readTrack());
    window.addEventListener("storage", onStorage);
    window.addEventListener("rangerdanger.firewall-track-changed", onCustom);
    return () => {
      window.removeEventListener("storage", onStorage);
      window.removeEventListener(
        "rangerdanger.firewall-track-changed",
        onCustom,
      );
    };
  }, []);

  const setTrack = useCallback((t: FirewallTrack) => {
    if (typeof window === "undefined") return;
    if (t === null) window.localStorage.removeItem(STORAGE_KEY);
    else window.localStorage.setItem(STORAGE_KEY, t);
    setTrackState(t);
    // Custom event so other components in the same tab pick up the
    // change immediately (the storage event only fires cross-tab).
    window.dispatchEvent(new Event("rangerdanger.firewall-track-changed"));
  }, []);

  return { track, setTrack };
}
