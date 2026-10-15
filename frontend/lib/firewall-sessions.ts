// Firewall session tuples for the instructor page: which conntrack
// entry is the control centre's IEC 104 session, and what a kill did.
//
// lib60870 cannot report the CC's local address, so the CC's session
// is found in containd's own list: the entry from the CC to the RTU on
// tcp/2404. Exactly one entry must match; zero or several is an error,
// never a guess, and nothing is killed.

import { ApiError } from "./api";
import { getFirewallSessions, type FirewallSession, type SessionTuple } from "./operator-api";
import { isRangeNotReady } from "./range";
import { errorMessage } from "./utils";

export const IEC104_PORT = 2404;
// The CC's address on control_centre_net and the RTU's on station_net
// (contract §1).
export const CONTROL_CENTRE_IP = "10.60.60.20";
export const RTU_IP = "10.30.30.20";

// A listed session as a kill tuple; null when containd left out a port.
export function sessionTuple(session: FirewallSession): SessionTuple | null {
  const { proto, src, dst, sport, dport } = session;
  if (sport === null || dport === null) return null;
  return { proto, src, dst, sport, dport };
}

export function isControlCentreSession(session: FirewallSession): boolean {
  return session.src === CONTROL_CENTRE_IP && session.dst === RTU_IP && session.dport === IEC104_PORT;
}

export type ControlCentreMatch =
  | { kind: "one"; tuple: SessionTuple }
  | { kind: "none" }
  | { kind: "several"; count: number }
  | { kind: "incomplete" };

// The CC's session among listed ones. "incomplete": the one match has
// no source port, so there is no exact tuple to delete.
export function matchControlCentreSession(sessions: FirewallSession[]): ControlCentreMatch {
  const matches = sessions.filter(isControlCentreSession);
  if (matches.length === 0) return { kind: "none" };
  if (matches.length > 1) return { kind: "several", count: matches.length };
  const tuple = sessionTuple(matches[0]);
  return tuple ? { kind: "one", tuple } : { kind: "incomplete" };
}

const CC_FLOW = `tcp ${CONTROL_CENTRE_IP} → ${RTU_IP}:${IEC104_PORT}`;

export function controlCentreMatchText(match: ControlCentreMatch): string {
  switch (match.kind) {
    case "one":
      return tupleText(match.tuple);
    case "none":
      return `containd lists no ${CC_FLOW} session.`;
    case "several":
      return `containd lists ${match.count} ${CC_FLOW} sessions; the control centre's cannot be told apart.`;
    case "incomplete":
      return `containd lists the ${CC_FLOW} session without its source port.`;
  }
}

// The lookup found no single session; nothing was killed.
export class SessionLookupError extends Error {
  constructor(match: Exclude<ControlCentreMatch, { kind: "one" }>) {
    super(`${controlCentreMatchText(match)} Nothing was killed.`);
    this.name = "SessionLookupError";
  }
}

// Reads the CC's sessions fresh from containd and returns the one tuple
// to kill. Throws SessionLookupError unless exactly one matches.
export async function findControlCentreSession(): Promise<SessionTuple> {
  const list = await getFirewallSessions({ dport: IEC104_PORT, src: CONTROL_CENTRE_IP, dst: RTU_IP });
  const match = matchControlCentreSession(list.sessions);
  if (match.kind !== "one") throw new SessionLookupError(match);
  return match.tuple;
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

// What the cut button reports when the lookup or the kill failed.
export function cutFailureText(tuple: SessionTuple | null, error: unknown): string {
  if (error instanceof SessionLookupError) return error.message;
  if (tuple !== null) return killResultText(tuple, error);
  if (isRangeNotReady(error)) return error.message;
  return `Could not read the firewall sessions: ${errorMessage(error)}. Nothing was killed.`;
}
