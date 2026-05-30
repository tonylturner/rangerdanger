"use client";

import { useEffect, useState, useCallback } from "react";

// useFirewallTrack — student's choice of how to interact with containd
// across the firewall implementation labs (2.2 / 2.3 / 2.3-bonus / 2.4).
//
// guided   — student applies policies via side-panel buttons (Apply
//            Hardened, Apply Your Plan). Still walks the containd
//            interface for understanding, just doesn't author rules
//            themselves.
// technical — student authors and commits the policy directly in
//            containd's web UI or CLI. Side-panel buttons are
//            de-emphasized as a fallback. Tracked by the banner
//            ("Your custom policy") once a commit lands.
// null     — not yet picked. Lab 2.2 step 1 force-picks; later labs
//            inherit from localStorage.

export type FirewallTrack = "guided" | "technical" | null;

const STORAGE_KEY = "rangerdanger.firewall-track";

function readTrack(): FirewallTrack {
  if (typeof window === "undefined") return null;
  const v = window.localStorage.getItem(STORAGE_KEY);
  if (v === "guided" || v === "technical") return v;
  return null;
}

export function useFirewallTrack(): {
  track: FirewallTrack;
  setTrack: (t: FirewallTrack) => void;
} {
  const [track, setTrackState] = useState<FirewallTrack>(null);

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
