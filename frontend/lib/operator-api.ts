// Typed clients for the EU control-centre, instructor and firewall
// session routes. Shapes follow the increment-3 contract: the control
// centre (CC) state, commands and journal (§6) as the backend wraps
// them (§7.2), the feeder-breaker device state and audit (§4) and the
// solver output (§5). Timestamps are RFC3339 strings; null means "not
// known", never a default.

import { request } from "./api";

// ── Control centre (GET /api/operator/*) ─────────────────────────

export type LinkState = "disconnected" | "connecting" | "started";
export type GiState = "none" | "running" | "complete";

export type GeneralInterrogation = {
  state: GiState;
  started_at: string | null;
  completed_at: string | null;
};

export type ControlCentreLink = {
  state: LinkState;
  peer: string;
  // "<ip>:<port>" of the CC end of the current session; null when none.
  local: string | null;
  since: string | null;
  last_rx_at: string | null;
  connection_seq: number;
  gi: GeneralInterrogation;
};

// ov is null for status points (it only applies to measurements).
export type PointQuality = {
  iv: boolean;
  nt: boolean;
  sb: boolean;
  bl: boolean;
  ov: boolean | null;
};

export type StaleReason = "link_lost" | "device_unreachable" | "measurement_invalid" | "never_received";
export type DoublePointValue = "intermediate" | "open" | "closed" | "indeterminate";
// Double points carry their state name, single points a bool, floats a
// number; null only when the CC has never received the point.
export type PointValue = DoublePointValue | boolean | number | null;

export type OperatorPoint = {
  id: string;
  ioa: number;
  asdu: string;
  value: PointValue;
  quality: PointQuality;
  stale_reason: StaleReason | null;
  // RTU observation time (CP56Time2a); null for untimed ASDUs.
  source_time: string | null;
  source_time_invalid: boolean | null;
  received_at: string | null;
  cot: number | null;
};

export type OperatorState = {
  schema: number;
  cache_epoch: string;
  link: ControlCentreLink;
  points: OperatorPoint[];
  package_id: string;
  range_generation: number;
};

export type CommandOperation = "open" | "close";
export type CommandStatus = "completed" | "rejected" | "failed" | "unknown";
export type CommandPhaseStatus = "confirmed" | "negative" | "timeout" | null;
export type FeedbackStatus = "matched" | "mismatched" | "missing";

export type CommandResult = {
  id: string;
  status: CommandStatus;
  select: { status: CommandPhaseStatus; at: string | null };
  execute: { status: CommandPhaseStatus; at: string | null; terminated_at: string | null };
  feedback: { status: FeedbackStatus; value: PointValue; received_at: string | null };
  error: string | null;
};

// The one command point of the slice (points.json `commands`).
export const BREAKER_COMMAND_POINT = "feeder-breaker-command";

// Synchronous: the CC answers after the whole select/execute/feedback
// sequence. 400/409/503 from the CC arrive as an ApiError.
export async function sendOperatorCommand(operation: CommandOperation) {
  return request<CommandResult>("/operator/commands", {
    method: "POST",
    body: JSON.stringify({ point: BREAKER_COMMAND_POINT, operation }),
  });
}

export async function getOperatorState() {
  return request<OperatorState>("/operator/state");
}

// detail differs per kind (link_up, link_down, gi_started, gi_complete,
// point_stale, point_good, command); lib/operator-format.ts reads it.
export type JournalEntry = {
  seq: number;
  at: string;
  kind: string;
  detail: Record<string, unknown>;
};

export type OperatorJournal = {
  cache_epoch: string;
  entries: JournalEntry[];
  latest_seq: number;
  oldest_seq: number;
};

export async function getOperatorJournal() {
  return request<OperatorJournal>("/operator/journal?after=0");
}

// ── Instructor truth (GET /api/instructor/*) ─────────────────────

export type DeviceMeasurements = {
  current_a: number | null;
  voltage_kv_ll: number | null;
  valid: boolean;
  solver_epoch: string | null;
  solve_seq: number | null;
  solved_at: string | null;
  received_at: string | null;
};

export type BreakerPosition = "open" | "closed";

export type DeviceState = {
  device_id: string;
  generation: number;
  revision: number;
  position: BreakerPosition;
  remote_enabled: boolean;
  lockout: boolean;
  fault_active: boolean;
  protection_tripped: boolean;
  event_seq: number;
  changed_at: string;
  measurements: DeviceMeasurements;
};

export type ProcessElectrical = {
  schema: number;
  profile_id: string;
  frequency_hz: number;
  primary_kv: number;
  breaker: BreakerPosition;
  source_voltage_kv_ll: number;
  load_voltage_kv_ll: number;
  current_a: number;
  load_kw: number;
  energized: boolean;
  converged: boolean;
  solver_epoch: string;
  solve_seq: number;
  solved_at: string;
  truth_revision: number;
  truth_generation: number;
  truth_stale: boolean;
};

export type InstructorState = {
  device: DeviceState;
  process: ProcessElectrical;
};

export type DeviceAuditEntry = {
  seq: number;
  at: string;
  operation: string;
  result: string;
  before: Record<string, unknown> | null;
  after: Record<string, unknown> | null;
  context: { peer: string; protocol: string; zone: string };
  detail?: Record<string, unknown>;
};

export type DeviceAudit = {
  generation: number;
  entries: DeviceAuditEntry[];
  latest_seq: number;
  oldest_seq: number;
};

export async function getInstructorState() {
  return request<InstructorState>("/instructor/state");
}

export async function getInstructorAudit() {
  return request<DeviceAudit>("/instructor/audit?after=0");
}

export async function setInstructorFault(active: boolean) {
  return request<DeviceState>("/instructor/fault", {
    method: "POST",
    body: JSON.stringify({ active }),
  });
}

export async function resetInstructorDevice() {
  return request<DeviceState>("/instructor/reset", { method: "POST", body: "{}" });
}

// ── Firewall sessions (GET /api/firewall/sessions) ───────────────

// One containd conntrack entry; the backend turns its ports into ints.
export type FirewallSession = {
  proto: string;
  state?: string;
  src?: string;
  dst?: string;
  sport?: number;
  dport?: number;
  replySrc?: string;
  replyDst?: string;
  replySport?: number;
  replyDport?: number;
  assured?: boolean;
  timeoutSecs?: number;
};

// The original-direction tuple containd deletes. Zero ports are not
// wildcards, so a kill always names both.
export type SessionTuple = {
  proto: string;
  src: string;
  dst: string;
  sport: number;
  dport: number;
};

export async function getFirewallSessions(dport: number) {
  return request<FirewallSession[]>(`/firewall/sessions?dport=${dport}`);
}

// 404 when containd has no such entry.
export async function killFirewallSession(tuple: SessionTuple) {
  return request<{ status: string }>("/firewall/sessions/kill", {
    method: "POST",
    body: JSON.stringify(tuple),
  });
}
