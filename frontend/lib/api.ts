import { API_BASE_URL } from "./utils";
import { parseRangeNotReady, reportRangeNotReady, type RangeStatus } from "./range";

// A non-2xx answer. detail is the server's {"error"} text when it sent
// one; the message carries it, so every banner shows the backend's cause.
export class ApiError extends Error {
  readonly status: number;
  readonly detail: string;
  readonly warnings: string[];

  constructor(status: number, detail: string, warnings: string[] = []) {
    super(detail ? `${status} ${detail}` : `Request failed: ${status}`);
    this.status = status;
    this.detail = detail;
    this.warnings = warnings;
  }
}

async function errorBody(res: Response): Promise<unknown> {
  try {
    return await res.json();
  } catch {
    return null;
  }
}

function errorDetail(body: unknown): string {
  if (typeof body !== "object" || body === null) return "";
  const { error, reason } = body as { error?: unknown; reason?: unknown };
  return typeof error === "string" ? error : typeof reason === "string" ? reason : "";
}

// Every API helper goes through here (lib/operator-api.ts too), so a
// not-ready range is reported the same way for all of them.
export async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${API_BASE_URL}${path}`, {
    headers: {
      "Content-Type": "application/json",
      ...(init?.headers ?? {})
    },
    cache: "no-store",
    ...init
  });

  if (!res.ok) {
    const body = await errorBody(res);
    const notReady = res.status === 503 ? parseRangeNotReady(body) : null;
    if (notReady) {
      reportRangeNotReady(notReady);
      throw notReady;
    }
    const warnings = (body as { warnings?: unknown } | null)?.warnings;
    throw new ApiError(res.status, errorDetail(body), Array.isArray(warnings) ? warnings.filter((w): w is string => typeof w === "string") : []);
  }

  if (res.status === 204) {
    return undefined as T;
  }

  return res.json();
}

export type GraphNodeData = {
  label: string;
  zone: string;
  networks: string[];
  status?: string;
  ip?: string;
  interface_ips?: Record<string, string>; // network -> IP for multi-homed nodes
  ui_path?: string;
  external_ui_url?: string; // direct URL for external UI access
};

export type GraphNode = {
  id: string;
  type: string;
  position: { x: number; y: number };
  data: GraphNodeData;
};

export type GraphEdge = { id: string; source: string; target: string; label?: string };
export type TopologyFlow = {
  id: string;
  source: string;
  target: string;
  protocol: string;
  port: number;
  cadence: string;
  category: "baseline" | "scenario";
  liveness_key: string;
};
export type LivenessBinding =
  | { provider: "workshop-supervisor" | "traffic-generator" | "operator-link" | "unobserved" }
  | { provider: "workshop-device"; device: string }
  | { provider: "operator-device"; points: string[] }
  | { provider: "session-presence"; transport: "tcp" | "udp" };
export type TopologyNetwork = {
  id: string;
  name: string;
  zone?: string;
  subnet?: string;
  cidr?: string;
  description?: string;
};
export type LabGraph = {
  nodes: GraphNode[];
  edges: GraphEdge[];
  flows: TopologyFlow[];
  liveness: Record<string, LivenessBinding>;
  networks: TopologyNetwork[];
  operations: { traffic_generation: boolean };
};

export type DecisionRole = {
  name: string;
  capacity_hours: number;
};

export type DecisionAction = {
  id: string;
  title: string;
  why: string;
  effort_hours: number;
  roles: string[];
  tags?: string[];
};

export type StepAction = {
  type: "command" | "check" | "firewall" | "sequence" | "manual" | "decision" | "probe" | "session-cut";
  device?: string;
  command?: string;
  source?: string;
  value?: number;
  flow?: string;
  config?: string;
  expect?: Record<string, unknown>;
  outcome?: "reachable" | "blocked";
  targets?: { from?: string; host: string; port: number; note?: string }[];
  commands?: { device: string; command: string; source?: string; value?: number }[];
};

// id is the authored, stable step identifier (unique in its scenario).
// Progress is stored by it; the execute route stays index-based.
export type ScenarioStep = { id: string; title: string; description: string; expected_config?: string; action?: StepAction; node?: string };
export type Scenario = {
  id: string;
  name: string;
  summary?: string;
  description: string;
  order: string;
  package_id: string;
  lab_template_id: string;
  // Validator key declared by the scenario; empty when it has none.
  validator: string;
  tags: string[];
  steps: ScenarioStep[];
  nodes?: string[];
  estimated_minutes?: number;
};

type RawScenario = Omit<Scenario, "tags" | "steps" | "nodes"> & { tags: string; steps: string; nodes?: string };

function safeParse<T>(value: string | null | undefined, fallback: T): T {
  if (!value) return fallback;
  try {
    return JSON.parse(value) as T;
  } catch (err) {
    return fallback;
  }
}

function hydrateScenario(raw: RawScenario): Scenario {
  return {
    ...raw,
    tags: safeParse<string[]>(raw.tags, []),
    steps: safeParse<ScenarioStep[]>(raw.steps, []),
    nodes: safeParse<string[]>(raw.nodes, []),
  };
}

// The backend serves only the active package's scenarios.
export async function listScenarios() {
  const res = await request<{ scenarios: RawScenario[] }>("/scenarios");
  return { scenarios: res.scenarios.map(hydrateScenario) };
}

// A curriculum package as GET /api/packages reports it. revision is the
// curriculum revision: it changes when step IDs or validator meaning do.
// operator_view names the operator screen the package uses; it stays a
// string because the portal must show an error for a value it does not
// know (lib/operator-view.ts), never fall back to another view.
// policies are the package.yml policy names /firewall/apply accepts,
// the package's default first (lib/policy-wording.ts).
export type PackageSummary = {
  curriculum?: CurriculumBinding;
  id: string;
  title: string;
  revision: number;
  active: boolean;
  operator_view: string;
  policies: string[];
};

export async function listPackages() {
  return request<PackageSummary[]>("/packages");
}

export async function getActivePackage(): Promise<PackageSummary> {
  const active = (await listPackages()).find((p) => p.active);
  if (!active) throw new Error("The backend reports no active curriculum package");
  return active;
}

export async function getRange() {
  return request<RangeStatus>("/range");
}

// Starts, restarts or switches the range (202 with the new status). 409
// means another transition is already running; 400 an unknown package.
export async function requestRange(packageId: string) {
  return request<RangeStatus>("/range", {
    method: "POST",
    body: JSON.stringify({ package: packageId }),
  });
}

// Firewall rule summaries for topology edge labels
export type ZoneRuleSummary = {
  source_zone: string;
  dest_zone: string;
  summary: string;
  rule_details: string[];
  action: "ALLOW" | "DENY" | "MIXED";
};

export type FirewallRulesResponse = {
  summaries: ZoneRuleSummary[];
  source: "containd" | "static";
  error?: string;
};

export async function getFirewallRules() {
  return request<FirewallRulesResponse>("/firewall/rules");
}

// Substation data types (from rtac-sim via backend proxy)
export type SubstationTags = {
  tags: Record<string, number | boolean | string>;
  last_poll: string;
};

export type PhysicsStatus = {
  stale: boolean;
  consecutive_failures: number;
  last_good_at: string;
  solved_at: string;
  last_error: string;
};

export type SubstationState = {
  devices: {
    relay?: Record<string, number | boolean | string>;
    recloser?: Record<string, number | boolean | string>;
    regulator?: Record<string, number | boolean | string>;
    capbank?: Record<string, number | boolean | string>;
  };
  electrical: {
    substation_bus_voltage_kv?: number;
    substation_bus_voltage_v?: number;
    downstream_voltage_v?: number;
    critical_load_voltage_v?: number;
    feeder_current_a?: number;
    general_load_energized?: boolean;
    critical_load_energized?: boolean;
    general_load_kw?: number;
    critical_load_kw?: number;
    breaker_closed?: boolean;
    recloser_closed?: boolean;
    regulator_tap?: number;
    capbank_switched_in?: boolean;
    // OpenDSS power flow fields
    converged?: boolean;
    solved_at?: string;
    total_losses_kw?: number;
    power_factor?: number;
    source_power_kw?: number;
    fault_current_a?: number;
  };
  physics: PhysicsStatus;
  device_comms: Record<string, boolean>;
  last_poll: string;
};

export type AuditEntry = {
  timestamp: string;
  source: string;
  source_zone: string;
  target: string;
  command: string;
  result: string;
  detail: string;
  process_impact: string;
};

export async function getSubstationTags() {
  return request<SubstationTags>("/substation/tags");
}

export async function getSubstationState() {
  return request<SubstationState>("/substation/state");
}

export async function sendSubstationCommand(device: string, command: string, source?: string, value?: number) {
  return request<{ result: string; detail: string; process_impact?: string; source_zone?: string }>(`/substation/command/${device}`, {
    method: "POST",
    body: JSON.stringify({ command, source: source || "web-ui", ...(value !== undefined && { value }) }),
  });
}

export async function getSubstationAudit() {
  return request<{ entries: AuditEntry[] }>("/substation/audit");
}

// Load Simulator (training-only) load override. Fields are optional so a caller
// can patch just the override (during a ramp) without emitting audit noise, or
// pass audit_* on discrete user actions to log a single Command Audit entry.
export async function sendLabControl(body: {
  active?: boolean;
  general_load_pct?: number;
  critical_load_pct?: number;
  power_factor?: number;
  source?: string;
  audit_command?: string;
  audit_target?: string;
  audit_detail?: string;
}) {
  return request<{ result: string; lab?: Record<string, number | boolean> }>("/substation/lab-control", {
    method: "POST",
    body: JSON.stringify(body),
  });
}

export async function getSubstationHealth() {
  return request<{
    status: string;
    service: string;
    device_comms: Record<string, boolean>;
    last_poll: string;
    physics: PhysicsStatus;
  }>("/substation/health");
}

// Network DPI events from containd filtered to substation traffic.
//
// containd events use camelCase keys and semantic data in a nested
// `attributes` object. The backend maps containd's `proto` to `protocol`.
//
// Three event `kind` values reach this endpoint:
//   - "firewall.rule.hit" — nft drop/accept with attributes.action +
//     attributes.ruleId; this is what students see as the segmentation
//     enforcement signal in /console.
//   - "request"           — Modbus/DNP3/etc. DPI decoder events with
//     attributes.function_code / raw_hex; no action field (the rule.hit
//     event for the same packet carries the verdict).
//   - "anomaly"           — IDS protocol-violation alerts with
//     attributes.anomaly_type / message / severity.
export type NetworkEventAttributes = {
  action?: "ALLOW" | "DENY";   // firewall.rule.hit only
  ruleId?: string;             // firewall.rule.hit only
  proto?: string;
  port?: string;
  srcZone?: string;
  dstZone?: string;
  via?: string;
  function_code?: number;      // Modbus/DNP3 "request" kind
  raw_hex?: string;            // "request" kind
  unit_id?: number;
  is_write?: boolean;
  anomaly_type?: string;       // "anomaly" kind
  message?: string;            // "anomaly" kind
  severity?: string;           // "anomaly" kind
};

export type NetworkEvent = {
  id: string;
  timestamp: string;
  kind?: string;
  srcIp?: string;
  dstIp?: string;
  srcPort?: number;
  dstPort?: number;
  transport?: string;
  protocol?: string;
  attributes?: NetworkEventAttributes;
};

export async function getSubstationNetworkEvents() {
  return request<{ events: NetworkEvent[]; source: string; message?: string }>("/substation/network-events");
}

// Firewall policy comparison (weak vs improved)
export type PolicyRuleDiff = {
  zone_pair: string;
  weak_rule: string;
  improved_rule: string;
  weak_action: string;
  improved_action: string;
  change: "tightened" | "added" | "removed" | "unchanged";
};

export type PolicyComparison = {
  weak_config: string;
  improved_config: string;
  diffs: PolicyRuleDiff[];
  summary: string;
};

export async function getFirewallComparison() {
  return request<PolicyComparison>("/firewall/compare");
}

export type PolicySource =
  | "weak"
  | "hardened-reference"
  | "plan"
  | "manual-custom"
  | "";

export async function getActiveFirewallConfig() {
  return request<{ active_config: string; policy_source: PolicySource; plan?: PlanProvenance }>(
    "/firewall/active",
  );
}

export type ValidationCheck = {
  name: string;
  status: "pass" | "fail" | "warn";
  detail: string;
};

export type ValidationResult = {
  scenario_id: string;
  outcome: "PASS" | "FAIL" | "PARTIAL";
  checks: ValidationCheck[];
  timestamp: string;
};

export async function validateScenario(scenarioId: string) {
  return request<ValidationResult>(`/scenarios/${encodeURIComponent(scenarioId)}/validate`);
}

// config is one of the active package's policy names.
export async function applyFirewallConfig(config: string) {
  return request<{ status: string; active_config: string }>("/firewall/apply", {
    method: "POST",
    body: JSON.stringify({ config }),
  });
}

export type PlanLabel = "weak" | "partial" | "hardened";
export type PlanRequest = { packageId: string; revision: number; actionIds: string[] };
export type PlanProvenance = PlanRequest & { label: PlanLabel; policyHash: string; appliedAt: string };
export type PlanAction = { id: string; kind: "enforce" | "detect" | "plan"; chosen: boolean };
export type PlanRule = {
  id: string;
  description?: string;
  sourceZones?: string[];
  destZones?: string[];
  sources?: string[];
  destinations?: string[];
  protocols?: { name: string; port?: string }[];
  ics?: PlanICS;
  action: "ALLOW" | "DENY";
  log?: boolean;
};
export type PlanExpectation = {
  id: string; from: string; to: string; network: string; port: number;
  category: "authorized" | "unauthorized"; owners: string[]; labs?: string[];
  note: string; planVerdict: "allow" | "deny"; hardenedVerdict: "allow" | "deny";
  bridged: boolean; fromIp: string; toIp: string;
};
export type CompiledPlan = {
  packageId: string; revision: number; label: PlanLabel; policyHash: string;
  actions: PlanAction[];
  rules: { rule: PlanRule; origin: { kind: "required" | "selected" | "deferred"; action?: string } }[];
  expectations: PlanExpectation[];
  config: Record<string, unknown>;
};
export async function compilePlan(body: PlanRequest): Promise<CompiledPlan> {
  return request<CompiledPlan>("/plan/compile", { method: "POST", body: JSON.stringify(body) });
}
export async function applyPlan(body: PlanRequest) {
  return request<{ applied: boolean; warnings: string[]; plan: CompiledPlan }>("/firewall/apply-plan", {
    method: "POST", body: JSON.stringify(body),
  });
}
export type ProofRow = { id: string; expected: "allow" | "deny"; observed: "allow" | "deny" | "skipped"; pass: boolean };

export type ValidationReport = {
  planProof: { applicable: boolean; pass: boolean; policyHash: string; hashStable: boolean; skipped: number; rows: ProofRow[] };
  acceptance: { pass: boolean; skipped: number; rows: ProofRow[]; preventionGaps: string[] };
  markdown: string;
  summary: {
    authorized_pass: number;
    authorized_total: number;
    unauthorized_pass: number;
    unauthorized_total: number;
    result: string;
  };
  active_config: string;
  policy_source: string;
  pcap_path: string;
  policy_hash: string;
  window_start: string;
  window_end: string;
  policy_stable: boolean;
};

// runValidationReport runs the positive/negative segmentation test matrix
// against the CURRENTLY ACTIVE policy and returns a markdown report +
// summary. Backs Lab 2.4's "Generate Validation Report" button.
export async function runValidationReport(): Promise<ValidationReport> {
  return request<ValidationReport>("/firewall/validation-report", {
    method: "POST",
  });
}

export type StepActionResult = {
  action: string;
  success: boolean;
  detail: string;
  impact?: string;
};

export type StepExecutionResult = {
  step_index: number;
  step_title: string;
  action_type: string;
  success: boolean;
  results: StepActionResult[];
  timestamp: string;
};

export async function executeScenarioStep(scenarioId: string, stepIdx: number) {
  return request<StepExecutionResult>(`/scenarios/${encodeURIComponent(scenarioId)}/steps/${stepIdx}/execute`, {
    method: "POST",
  });
}

// PCAP capture (via containd /api/v1/pcap/* with tcpdump fallback)
export type PcapStatus = {
  capturing: boolean;
  duration_sec: number;
  started_at: string;
  file_ready: boolean;
  file_prefix?: string;
  files?: string[];       // filenames from containd (one per interface)
  last_error?: string;
};

export type PcapFileInfo = {
  name: string;
  interface: string;
  sizeBytes: number;
  createdAt: string;
  tags: string[];
  status: string;
};

export async function startPcapCapture(durationSec?: number, name?: string) {
  return request<{ status: string; duration_sec: number; file_prefix?: string }>("/pcap/start", {
    method: "POST",
    body: JSON.stringify({ duration_sec: durationSec || 30, name: name || "" }),
  });
}

export async function stopPcapCapture() {
  return request<{ status: string; files?: string[] }>("/pcap/stop", { method: "POST" });
}

export async function getPcapStatus() {
  return request<PcapStatus>("/pcap/status");
}

export async function listPcapFiles() {
  return request<{ files: PcapFileInfo[] }>("/pcap/list");
}

export function getPcapDownloadUrl(filename?: string) {
  if (filename) {
    return `${API_BASE_URL}/pcap/download/${encodeURIComponent(filename)}`;
  }
  return `${API_BASE_URL}/pcap/download`;
}

// Traffic generation
export type TrafficStatus = {
  generating: boolean;
  started_at: string;
  flows_generated: number;
  run_id: string;
};

export async function startTrafficGeneration(durationSec?: number) {
  return request<{ status: string; duration_sec: number; run_id: string; started_at: string }>("/traffic/generate", {
    method: "POST",
    body: JSON.stringify({ duration_sec: durationSec || 30 }),
  });
}

export async function stopTrafficGeneration(runID: string) {
  return request<{ status: "stopping" | "stopped" }>("/traffic/stop", {
    method: "POST",
    body: JSON.stringify({ run_id: runID }),
  });
}

export async function getTrafficStatus() {
  return request<TrafficStatus>("/traffic/status");
}

// The control centre's link and GI states; null while it does not answer.
export type WorkshopControlCentre = {
  link: "disconnected" | "connecting" | "started" | null;
  gi: "none" | "running" | "complete" | null;
};

// The package's supervisory service decides the optional part: an RTAC
// package (US) sends rtac_online and device_comms, a control-centre
// package (EU) sends control_centre.
export type WorkshopStatus = {
  workshop_id: string;
  workshop_name: string;
  firewall_online: boolean;
  firewall_config: string;
  scenario_count: number;
  rtac_online?: boolean;
  device_comms?: Record<string, boolean>;
  control_centre?: WorkshopControlCentre;
};

export async function getWorkshopGraph(): Promise<LabGraph> {
  return request<LabGraph>("/workshop/graph");
}

export async function getWorkshopStatus(): Promise<WorkshopStatus> {
  return request<WorkshopStatus>("/workshop/status");
}

// ── Exercise Exec & Reset ────────────────────────────────────────

export type ExecResult = {
  stdout: string;
  stderr: string;
  exit_code: number;
  duration_ms: number;
};

export async function execOnNode(nodeId: string, command: string, timeoutSec?: number) {
  return request<ExecResult>(`/workshop/nodes/${encodeURIComponent(nodeId)}/exec`, {
    method: "POST",
    body: JSON.stringify({ command, timeout_sec: timeoutSec || 30 }),
  });
}

export type ResetAction = {
  action: string;
  success: boolean;
  detail: string;
};

export type ResetResult = {
  success: boolean;
  actions: ResetAction[];
};

export async function resetWorkshop() {
  return request<ResetResult>("/workshop/reset", { method: "POST" });
}

// The backend strictly decodes the package sibling file and resolves topology
// references before publishing this model. Missing binding means no planner.
export type CurriculumBinding = {
  schema: 1;
  roles: Record<string, string>;
  activities: Record<string, { policyActions: boolean; hideBanner: boolean; validate?: boolean }>;
  dynamicSteps: Record<string, "rules" | "positive" | "negative">;
  icsRules: "allowed" | "forbidden";
  requirementOrder: string[];
  groupOrder: string[];
  impactOrder: string[];
  requirements: Record<string, { label: string; impl: Record<string, string[]> }>;
  readiness: { id: string; label: string; good: string }[];
  readinessFlags: { actions: string[]; severity: "warn" | "blocked"; source: string; sourceLabel: string; verdict: string; message: string }[];
  budgetHours: number;
  teams: DecisionRole[];
  catalog: DecisionAction[];
  impact: Record<string, { defends: string[]; note: string }>;
  groups: Record<string, { title: string; description: string; effect: "closes" | "restricts" | "semantic" | "conceptual" | "logging" }>;
  planningOnly: string[];
  phases: Record<string, string>;
  noPlan: string;
  tests: { kind: "positive" | "negative"; text: string }[];
  negativeEmpty: string;
  nodeMap: Record<string, { primary: string; others: string[] }>;
  nodeInference: { node: string; ip: string; pattern: string }[];
  dpiNotice: { title: string; description: string };
  dpiForbiddenNotice: { title: string; description: string };
};

export type PlanICS = {
  protocol?: string;
  functionCode?: number[];
  unitId?: number;
  addresses?: string[];
  objectClasses?: number[];
  readOnly?: boolean;
  writeOnly?: boolean;
  direction?: string;
  mode?: string;
};
