import { API_BASE_URL } from "./utils";

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${API_BASE_URL}${path}`, {
    headers: {
      "Content-Type": "application/json",
      ...(init?.headers ?? {})
    },
    cache: "no-store",
    ...init
  });

  if (!res.ok) {
    throw new Error(`Request failed: ${res.status}`);
  }

  if (res.status === 204) {
    return undefined as T;
  }

  return res.json();
}

export type LabInstance = {
  id: string;
  name: string;
  status: string;
  template_id: string;
  docker_stack_name?: string;
  runtime_config?: string;
  created_at?: string;
  updated_at?: string;
};

export type LabTemplate = {
  id: string;
  name: string;
  description: string;
  topology: string;
  default_scenarios?: string;
  compose_file?: string;
};

export type NodeDefinition = {
  id: string;
  lab_instance_id: string;
  type: string;
  name: string;
  ip: string;
  status: string;
  metadata?: string;
};

export type LabInstanceDetail = LabInstance & {
  template?: LabTemplate;
  nodes?: NodeDefinition[];
};

export type LabTopologyNetwork = { name: string; cidr: string };
export type LabTopologyNode = { id: string; name: string; type: string; networks: string[] };
export type LabTopology = {
  networks: LabTopologyNetwork[];
  nodes: LabTopologyNode[];
  scenarios?: Scenario[];
};

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
export type LabGraph = { nodes: GraphNode[]; edges: GraphEdge[] };

export type StepAction = {
  type: "command" | "check" | "firewall" | "sequence" | "manual";
  device?: string;
  command?: string;
  source?: string;
  value?: number;
  config?: string;
  expect?: Record<string, unknown>;
  commands?: { device: string; command: string; source?: string; value?: number }[];
};

export type ScenarioStep = { title: string; description: string; action?: StepAction; node?: string };
export type Scenario = {
  id: string;
  name: string;
  summary?: string;
  description: string;
  order?: number;
  lab_template_id: string;
  tags: string[];
  steps: ScenarioStep[];
  nodes?: string[];
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

export async function listLabInstances() {
  return request<{ instances: LabInstance[] }>("/labs/instances");
}

export async function listLabTemplates() {
  return request<{ templates: LabTemplate[] }>("/labs/templates");
}

export async function getLabInstance(id: string) {
  return request<LabInstanceDetail>(`/labs/instances/${id}`);
}

export async function getLabTopology(id: string) {
  const res = await request<{ topology: LabTopology }>(`/labs/instances/${id}/topology`);
  return res.topology;
}

export async function getLabGraph(id: string) {
  return request<LabGraph>(`/labs/instances/${id}/graph`);
}

export async function createLabInstance(templateId: string, name: string) {
  return request<LabInstance>("/labs/instances", {
    method: "POST",
    body: JSON.stringify({ template_id: templateId, name })
  });
}

export async function seedTemplates() {
  return request<{ status: string }>("/admin/seed", { method: "POST" });
}

export async function startLabInstance(id: string) {
  return request<{ status: string }>(`/labs/instances/${id}/start`, { method: "POST" });
}

export async function stopLabInstance(id: string) {
  return request<{ status: string }>(`/labs/instances/${id}/stop`, { method: "POST" });
}

export async function deleteLabInstance(id: string) {
  return request<void>(`/labs/instances/${id}`, { method: "DELETE" });
}

export async function listScenarios(templateId?: string) {
  const query = templateId ? `?lab_template_id=${encodeURIComponent(templateId)}` : "";
  const res = await request<{ scenarios: RawScenario[] }>(`/scenarios${query}`);
  return { scenarios: res.scenarios.map(hydrateScenario) };
}

export async function saveLabTemplate(payload: { id?: string; name: string; description: string; topology: unknown }) {
  return request<LabTemplate>("/labs/templates", {
    method: "POST",
    body: JSON.stringify({
      id: payload.id,
      name: payload.name,
      description: payload.description,
      topology: JSON.stringify(payload.topology),
      compose_file: "deploy/docker-compose.yml",
      default_scenarios: JSON.stringify([])
    })
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

export type SubstationState = {
  devices: {
    relay?: Record<string, number | boolean | string>;
    recloser?: Record<string, number | boolean | string>;
    regulator?: Record<string, number | boolean | string>;
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
    // OpenDSS power flow fields
    total_losses_kw?: number;
    power_factor?: number;
    source_power_kw?: number;
    fault_current_a?: number;
  };
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

export async function getSubstationHealth() {
  return request<{
    status: string;
    service: string;
    device_comms: Record<string, boolean>;
    last_poll: string;
  }>("/substation/health");
}

// Network DPI events from containd filtered to substation traffic
export type NetworkEvent = {
  id: string;
  timestamp: string;
  type: string;
  source: string;
  dest: string;
  protocol: string;
  src_port: number;
  dst_port: number;
  details: string;
  severity: string;
  zone: string;
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

export async function getActiveFirewallConfig() {
  return request<{ active_config: string }>("/firewall/active");
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

export async function applyFirewallConfig(config: "weak" | "improved") {
  return request<{ status: string; active_config: string }>("/firewall/apply", {
    method: "POST",
    body: JSON.stringify({ config }),
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
};

export async function startTrafficGeneration(durationSec?: number) {
  return request<{ status: string; duration_sec: number }>("/traffic/generate", {
    method: "POST",
    body: JSON.stringify({ duration_sec: durationSec || 30 }),
  });
}

export async function getTrafficStatus() {
  return request<TrafficStatus>("/traffic/status");
}

export type WorkshopStatus = {
  workshop_id: string;
  workshop_name: string;
  rtac_online: boolean;
  firewall_online: boolean;
  firewall_config: string;
  scenario_count: number;
  device_comms: Record<string, boolean>;
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
