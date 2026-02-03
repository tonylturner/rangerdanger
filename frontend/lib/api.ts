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

export type ScenarioStep = { title: string; description: string };
export type Scenario = {
  id: string;
  name: string;
  description: string;
  lab_template_id: string;
  tags: string[];
  steps: ScenarioStep[];
};

type RawScenario = Omit<Scenario, "tags" | "steps"> & { tags: string; steps: string };

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
    steps: safeParse<ScenarioStep[]>(raw.steps, [])
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
