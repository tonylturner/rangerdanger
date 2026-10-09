// Firewall session tuples for the instructor page: which conntrack
// entry is the control centre's IEC 104 session, and what a kill did.

import { ApiError } from "./api";
import type { ControlCentreLink, FirewallSession, SessionTuple } from "./operator-api";
import { parseEndpoint } from "./operator-format";
import { isRangeNotReady } from "./range";
import { errorMessage } from "./utils";

export const IEC104_PORT = 2404;

// The original-direction tuple of the CC's current IEC 104 session.
export function controlCentreTuple(link: ControlCentreLink | undefined): SessionTuple | null {
  const local = parseEndpoint(link?.local ?? null);
  const peer = parseEndpoint(link?.peer ?? null);
  if (!local || !peer) return null;
  return { proto: "tcp", src: local.ip, dst: peer.ip, sport: local.port, dport: peer.port };
}

// A listed session as a kill tuple; null when containd left out a field.
export function sessionTuple(session: FirewallSession): SessionTuple | null {
  const { proto, src, dst, sport, dport } = session;
  if (!src || !dst || sport === undefined || dport === undefined) return null;
  return { proto, src, dst, sport, dport };
}

export function sameTuple(a: SessionTuple | null, b: SessionTuple | null): boolean {
  return (
    a !== null &&
    b !== null &&
    a.proto === b.proto &&
    a.src === b.src &&
    a.dst === b.dst &&
    a.sport === b.sport &&
    a.dport === b.dport
  );
}

export function tupleText(t: SessionTuple): string {
  return `${t.proto} ${t.src}:${t.sport} → ${t.dst}:${t.dport}`;
}

export function killResultText(tuple: SessionTuple, error: unknown): string {
  if (error === null) {
    return `Deleted ${tupleText(tuple)} from conntrack. Its next packet is a new flow and meets the active policy: the session dies only if the policy denies it.`;
  }
  if (isRangeNotReady(error)) return error.message;
  if (error instanceof ApiError && error.status === 404) return `containd has no entry ${tupleText(tuple)} (already gone).`;
  return `Kill of ${tupleText(tuple)} failed: ${errorMessage(error)}`;
}
