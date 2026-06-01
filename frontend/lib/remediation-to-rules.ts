// Maps remediation action IDs from Exercise 2 to concrete containd firewall
// rules for Exercise 3. The student's plan drives which rules they must build.

import { loadRemediationPlan, type RemediationPlan } from "./remediation-plan";

export type FirewallRule = {
  id: string;
  source: string;
  destination: string;
  proto: string;
  port: string;
  action: "ALLOW" | "DENY";
  purpose: string;
  log?: boolean;
  ics?: { protocol: string; functionCodes?: number[]; note?: string };
};

export type RuleGroup = {
  remediationId: string;
  title: string;
  description: string;
  rules: FirewallRule[];
};

// Always present regardless of plan — operations must work.
const BASELINE_RULES: FirewallRule[] = [
  {
    id: "rtac-to-field-modbus",
    source: "10.30.30.20",
    destination: "10.40.40.0/24",
    proto: "TCP",
    port: "502",
    action: "ALLOW",
    purpose: "RTAC Modbus polling to field devices",
  },
  {
    id: "rtac-to-field-dnp3",
    source: "10.30.30.20",
    destination: "10.40.40.0/24",
    proto: "TCP",
    port: "20000",
    action: "ALLOW",
    purpose: "RTAC DNP3 polling to field devices",
  },
  {
    id: "rtac-to-field-http",
    source: "10.30.30.20",
    destination: "10.40.40.0/24",
    proto: "TCP",
    port: "8080",
    action: "ALLOW",
    purpose: "RTAC HTTP API to field devices",
  },
  {
    id: "gps-to-field-ntp",
    source: "10.30.30.50",
    destination: "10.40.40.0/24",
    proto: "UDP",
    port: "123",
    action: "ALLOW",
    purpose: "GPS NTP time sync to field devices",
  },
];

// Each remediation action produces a group of firewall rules.
const REMEDIATION_RULE_MAP: Record<string, Omit<RuleGroup, "remediationId">> = {
  "block-enterprise-to-field": {
    title: "Block enterprise → field direct access",
    description:
      "Remove the paths from the corporate IT zone straight to field relays and reclosers.",
    rules: [
      {
        id: "deny-enterprise-to-field",
        source: "10.10.10.0/24",
        destination: "10.40.40.0/24",
        proto: "TCP",
        port: "502, 20000, 8080",
        action: "DENY",
        purpose: "Block enterprise direct Modbus/DNP3/HTTP to field devices",
        log: true,
      },
    ],
  },
  "block-enterprise-to-ot": {
    title: "Block enterprise → OT Operations ICS ports",
    description:
      "Remove enterprise access to HMI, RTAC, and OpenPLC on ICS and management ports.",
    rules: [
      {
        id: "deny-enterprise-to-ot",
        source: "10.10.10.0/24",
        destination: "10.30.30.0/24",
        proto: "TCP",
        port: "502, 20000, 8080, 1881",
        action: "DENY",
        purpose: "Block enterprise ICS/HMI access to OT Operations",
        log: true,
      },
    ],
  },
  "restrict-vendor-to-ot": {
    title: "Restrict vendor → OT to SSH/HTTPS only",
    description:
      "Vendor remote access limited to encrypted management protocols. No direct Modbus or HMI web.",
    rules: [
      {
        id: "vendor-to-ot-restricted",
        source: "10.20.20.0/24",
        destination: "10.30.30.0/24",
        proto: "TCP",
        port: "22, 443",
        action: "ALLOW",
        purpose: "Vendor management access (SSH/HTTPS only)",
      },
    ],
  },
  "block-vendor-to-field": {
    title: "Block vendor → field devices entirely",
    description:
      "No legitimate reason for the vendor DMZ to reach field devices directly.",
    rules: [
      {
        id: "deny-vendor-to-field",
        source: "10.20.20.0/24",
        destination: "10.40.40.0/24",
        proto: "TCP",
        port: "502, 20000, 8080",
        action: "DENY",
        purpose: "Block vendor direct access to field devices",
        log: true,
      },
    ],
  },
  "pin-rtac-to-field": {
    title: "Pin field-device access to RTAC source IP",
    description:
      "Only the RTAC (10.30.30.20) can reach field devices from OT Ops. The RTAC ALLOW rules above this DENY take priority (first-match), so RTAC traffic passes while all other OT Ops hosts are blocked.",
    rules: [
      {
        id: "deny-ot-to-field-non-rtac",
        source: "10.30.30.0/24",
        destination: "10.40.40.0/24",
        proto: "TCP",
        port: "502, 20000, 8080",
        action: "DENY",
        purpose: "Block non-RTAC OT hosts from field (RTAC ALLOW rules above take priority)",
        log: true,
      },
    ],
  },
  "modbus-dpi": {
    title: "Modbus ICS DPI — limit function codes",
    description:
      "Configure containd to filter Modbus by function code. Allow reads (FC1–4) broadly; restrict writes (FC5/6) to the RTAC only.",
    rules: [
      {
        id: "rtac-modbus-dpi",
        source: "10.30.30.20",
        destination: "10.40.40.0/24",
        proto: "TCP",
        port: "502",
        action: "ALLOW",
        purpose: "RTAC Modbus with DPI: FC1-6 (read + write)",
        ics: {
          protocol: "modbus",
          functionCodes: [1, 2, 3, 4, 5, 6],
          note: "Full read/write for supervisory control",
        },
      },
    ],
  },
  "dnp3-dpi": {
    title: "DNP3 ICS DPI — restrict Direct Operate",
    description:
      "Configure containd to recognize DNP3 and block Direct Operate from non-RTAC sources. Reads and integrity polls stay available.",
    rules: [
      {
        id: "rtac-dnp3-dpi",
        source: "10.30.30.20",
        destination: "10.40.40.0/24",
        proto: "TCP",
        port: "20000",
        action: "ALLOW",
        purpose: "RTAC DNP3 with DPI: all application functions",
        ics: {
          protocol: "dnp3",
          note: "Full DNP3 access including Direct Operate (FC05)",
        },
      },
    ],
  },
  "improve-logging": {
    title: "Enable firewall logging and PCAP retention",
    description:
      "Enable per-rule logging on all deny actions and critical allow rules. Without this, post-incident review is blind.",
    rules: [],
  },
  "tighten-firewall-objects": {
    title: "Tighten firewall object hygiene",
    description:
      "Replace broad /24 matches with /32 host objects for RTAC and GPS. Name and document each rule.",
    rules: [],
  },
  "positive-validation": {
    title: "Post-change positive/negative validation",
    description:
      "Build a test plan that verifies legitimate flows still work and illegitimate flows are blocked.",
    rules: [],
  },
  "rtac-architecture-review": {
    title: "Validate and document RTAC hardening",
    description:
      "Verify RTAC host-level hardening (ip_forward=0, FORWARD DROP, policy routing). Document as a compensating control.",
    rules: [],
  },
};

// Enterprise → vendor management is always needed if the student selects
// any vendor-zone remediation, but also reasonable as a baseline rule.
const ENTERPRISE_TO_VENDOR_RULE: FirewallRule = {
  id: "enterprise-to-vendor",
  source: "10.10.10.0/24",
  destination: "10.20.20.0/24",
  proto: "TCP",
  port: "22, 80, 443",
  action: "ALLOW",
  purpose: "Enterprise to vendor management (SSH/HTTP/HTTPS)",
};

export type ActionSummary = {
  id: string;
  title: string;
  description: string;
  hasRules: boolean;
};

export type DynamicExercisePlan = {
  hasRemediationPlan: boolean;
  selectedActions: string[];
  baselineRules: FirewallRule[];
  remediationGroups: RuleGroup[];
  enterpriseToVendorRule: FirewallRule;
  allRules: FirewallRule[];
  enableLogging: boolean;
  tightenObjects: boolean;
  includeValidation: boolean;
  includeDpi: boolean;
  selectedSummary: ActionSummary[];
  unselectedSummary: ActionSummary[];
};

export function buildDynamicPlan(plan: RemediationPlan | null): DynamicExercisePlan {
  const selected = plan?.selectedActionIds ?? [];
  const hasRemediationPlan = plan !== null && selected.length > 0;

  const groups: RuleGroup[] = [];
  for (const actionId of selected) {
    const mapping = REMEDIATION_RULE_MAP[actionId];
    if (mapping && mapping.rules.length > 0) {
      groups.push({ remediationId: actionId, ...mapping });
    }
  }

  const enableLogging = selected.includes("improve-logging");
  const tightenObjects = selected.includes("tighten-firewall-objects");
  const includeValidation = selected.includes("positive-validation");
  const includeDpi =
    selected.includes("modbus-dpi") || selected.includes("dnp3-dpi");

  // Collect all rules in order: baseline → enterprise-to-vendor → remediation groups
  const allRules: FirewallRule[] = [
    ...BASELINE_RULES,
    ENTERPRISE_TO_VENDOR_RULE,
    ...groups.flatMap((g) => g.rules),
  ];

  // If logging is selected, mark all DENY rules and RTAC ALLOW rules with log: true
  if (enableLogging) {
    for (const rule of allRules) {
      if (rule.action === "DENY" || rule.id.startsWith("rtac-")) {
        rule.log = true;
      }
    }
  }

  return {
    hasRemediationPlan,
    selectedActions: selected,
    baselineRules: BASELINE_RULES,
    remediationGroups: groups,
    enterpriseToVendorRule: ENTERPRISE_TO_VENDOR_RULE,
    allRules,
    enableLogging,
    tightenObjects,
    includeValidation,
    includeDpi,
    selectedSummary: selected.map((id) => {
      const m = REMEDIATION_RULE_MAP[id];
      return {
        id,
        title: m?.title ?? id,
        description: m?.description ?? "",
        hasRules: (m?.rules.length ?? 0) > 0,
      };
    }),
    unselectedSummary: Object.entries(REMEDIATION_RULE_MAP)
      .filter(([id]) => !selected.includes(id))
      .map(([id, m]) => ({
        id,
        title: m.title,
        description: m.description,
        hasRules: m.rules.length > 0,
      })),
  };
}

export function loadDynamicPlan(): DynamicExercisePlan {
  return buildDynamicPlan(loadRemediationPlan());
}

// Generate the markdown rule table for Exercise 3 Phase 3.
export function renderRuleTable(plan: DynamicExercisePlan): string {
  const rows = plan.allRules.map((r, i) => {
    const logCol = plan.enableLogging ? (r.log ? " Yes" : " —") : "";
    const icsCol = r.ics ? ` ${r.ics.note || r.ics.protocol}` : "";
    return `| ${i + 1} | \`${r.source}\` | \`${r.destination}\` | ${r.proto} | ${r.port} | ${r.action} | ${r.purpose} |${logCol}${icsCol}`;
  });

  const logHeader = plan.enableLogging ? " Log |" : "";
  const logSep = plan.enableLogging ? " --- |" : "";

  return [
    `| # | Source | Destination | Proto | Port(s) | Action | Purpose |${logHeader}`,
    `|---|--------|-------------|-------|---------|--------|---------|${logSep}`,
    ...rows,
  ].join("\n");
}

// Build positive validation test commands based on what the plan allows.
export function positiveValidationTests(plan: DynamicExercisePlan): string[] {
  // RTAC → field is always allowed
  const tests = [
    "# RTAC → field Modbus (should succeed):\nmbpoll -m tcp -a 1 -r 1 -c 5 -1 -t 1 10.40.40.20\n# Run from: rtac-1 or verify via RTAC API:\ncurl -s http://10.30.30.20:8080/api/state | python3 -m json.tool",
    "# HMI → RTAC intra-zone (should still work):\ncurl -sf http://10.30.30.10:1881 --connect-timeout 3",
  ];
  if (plan.selectedActions.includes("restrict-vendor-to-ot")) {
    tests.push(
      "# Vendor → OT SSH (should succeed):\nssh -o ConnectTimeout=3 eng@10.30.30.20 echo ok\n# Or test HTTPS if available"
    );
  }
  return tests;
}

// Build negative validation test commands based on what the plan blocks.
export function negativeValidationTests(plan: DynamicExercisePlan): string[] {
  const tests: string[] = [];

  if (plan.selectedActions.includes("block-enterprise-to-field")) {
    tests.push(
      "# Enterprise → field Modbus (should be BLOCKED):\n# From kali-1:\nmbpoll -m tcp -a 1 -r 1 -c 5 -1 -t 1 -o 3 10.40.40.20"
    );
    tests.push(
      "# Enterprise → field DNP3 (should be BLOCKED):\n# From kali-1:\ndnp3poll 10.40.40.20:20000 -a 1 -t 3"
    );
  }

  if (plan.selectedActions.includes("block-enterprise-to-ot")) {
    tests.push(
      "# Enterprise → OT Modbus (should be BLOCKED):\n# From kali-1:\nmbpoll -m tcp -a 1 -r 1 -c 5 -1 -t 1 -o 3 10.30.30.20"
    );
  }

  if (plan.selectedActions.includes("block-vendor-to-field")) {
    tests.push(
      "# Vendor → field Modbus (should be BLOCKED):\n# From eng-ws-1:\nmbpoll -m tcp -a 1 -r 1 -c 5 -1 -t 1 -o 3 10.40.40.23"
    );
    tests.push(
      "# Vendor → field HTTP (should be BLOCKED):\n# From eng-ws-1:\ncurl -sf --connect-timeout 3 http://10.40.40.22:8080/api/state"
    );
  }

  if (plan.selectedActions.includes("pin-rtac-to-field")) {
    tests.push(
      "# Non-RTAC OT host → field (should be BLOCKED):\n# From historian-1 or openplc-1 (any single-homed OT host):\nmbpoll -m tcp -a 1 -r 1 -c 5 -1 -t 1 -o 3 10.40.40.20"
    );
  }

  if (tests.length === 0) {
    tests.push(
      "# Your plan did not include cross-zone deny rules.\n# No negative tests to run — all cross-zone traffic is still permitted under the weak baseline.\n# Consider whether this is acceptable risk."
    );
  }

  return tests;
}

// Generate a containd-compatible firewall config JSON from the student's plan.
// Starts from the weak baseline structure and replaces rules.
export function buildContaindConfig(plan: DynamicExercisePlan): object {
  const rules: object[] = [
    {
      id: "allow-mgmt-ui",
      description: "Allow management UI/API access",
      protocols: [{ name: "tcp", port: "8080" }],
      ics: {},
      action: "ALLOW",
    },
    {
      id: "enterprise-to-vendor",
      description: "Enterprise to Vendor: SSH, HTTP/S",
      sourceZones: ["wan"],
      destZones: ["dmz"],
      protocols: [
        { name: "tcp", port: "22" },
        { name: "tcp", port: "80" },
        { name: "tcp", port: "443" },
      ],
      ics: {},
      action: "ALLOW",
    },
  ];

  const sel = new Set(plan.selectedActions);
  const log = sel.has("improve-logging");

  if (sel.has("block-enterprise-to-ot")) {
    rules.push({
      id: "deny-enterprise-to-ot",
      description: "BLOCK: Enterprise cannot reach OT Operations",
      sourceZones: ["wan"],
      destZones: ["lan1"],
      protocols: [
        { name: "tcp", port: "8080" },
        { name: "tcp", port: "1881" },
        { name: "tcp", port: "502" },
        { name: "tcp", port: "20000" },
      ],
      ics: {},
      action: "DENY",
      ...(log && { log: true }),
    });
  }

  if (sel.has("block-enterprise-to-field")) {
    rules.push({
      id: "deny-enterprise-to-field",
      description: "BLOCK: Enterprise cannot reach Field Devices",
      sourceZones: ["wan"],
      destZones: ["lan2"],
      protocols: [
        { name: "tcp", port: "8080" },
        { name: "tcp", port: "502" },
        { name: "tcp", port: "20000" },
      ],
      ics: {},
      action: "DENY",
      ...(log && { log: true }),
    });
  }

  if (sel.has("restrict-vendor-to-ot")) {
    rules.push({
      id: "vendor-to-ot-restricted",
      description: "Vendor to OT: SSH and HTTPS only",
      sourceZones: ["dmz"],
      destZones: ["lan1"],
      protocols: [
        { name: "tcp", port: "443" },
        { name: "tcp", port: "22" },
      ],
      ics: {},
      action: "ALLOW",
    });
  }

  if (sel.has("block-vendor-to-field")) {
    rules.push({
      id: "deny-vendor-to-field",
      description: "BLOCK: Vendor cannot reach Field Devices",
      sourceZones: ["dmz"],
      destZones: ["lan2"],
      protocols: [
        { name: "tcp", port: "8080" },
        { name: "tcp", port: "502" },
        { name: "tcp", port: "20000" },
      ],
      ics: {},
      action: "DENY",
      ...(log && { log: true }),
    });
  }

  // RTAC → field rules (always present)
  const rtacModbus: Record<string, unknown> = {
    id: "rtac-to-field-modbus",
    description: "RTAC to Field: Modbus TCP",
    sourceZones: ["lan1"],
    destZones: ["lan2"],
    sources: ["10.30.30.20/32"],
    protocols: [{ name: "tcp", port: "502" }],
    action: "ALLOW",
    ...(log && { log: true }),
  };
  if (sel.has("modbus-dpi")) {
    rtacModbus.ics = {
      // containd's ICS schema uses singular `functionCode` (see
      // substation-improved.json + containd ui/lib/api.ts); emitting
      // `functionCodes` here would be silently ignored, leaving the
      // Modbus DPI rule unfiltered.
      protocol: "modbus",
      functionCode: [1, 2, 3, 4, 5, 6],
    };
  } else {
    rtacModbus.ics = {};
  }
  rules.push(rtacModbus);

  const rtacDnp3: Record<string, unknown> = {
    id: "rtac-to-field-dnp3",
    description: "RTAC to Field: DNP3 TCP",
    sourceZones: ["lan1"],
    destZones: ["lan2"],
    sources: ["10.30.30.20/32"],
    protocols: [{ name: "tcp", port: "20000" }],
    action: "ALLOW",
    ...(log && { log: true }),
  };
  if (sel.has("dnp3-dpi")) {
    rtacDnp3.ics = { protocol: "dnp3" };
  } else {
    rtacDnp3.ics = {};
  }
  rules.push(rtacDnp3);

  rules.push({
    id: "rtac-to-field-http",
    description: "RTAC to Field: HTTP API",
    sourceZones: ["lan1"],
    destZones: ["lan2"],
    sources: ["10.30.30.20/32"],
    protocols: [{ name: "tcp", port: "8080" }],
    ics: {},
    action: "ALLOW",
    ...(log && { log: true }),
  });

  if (sel.has("pin-rtac-to-field")) {
    rules.push({
      id: "deny-ot-to-field-non-rtac",
      description:
        "BLOCK: OT Ops to Field deny-all (RTAC ALLOW rules above take first-match priority)",
      sourceZones: ["lan1"],
      destZones: ["lan2"],
      protocols: [
        { name: "tcp", port: "8080" },
        { name: "tcp", port: "502" },
        { name: "tcp", port: "20000" },
      ],
      ics: {},
      action: "DENY",
      ...(log && { log: true }),
    });
  }

  // Intra-zone and infrastructure
  rules.push(
    {
      id: "ot-intrazone",
      description: "OT Operations internal traffic",
      sourceZones: ["lan1"],
      destZones: ["lan1"],
      protocols: [
        { name: "tcp", port: "8080" },
        { name: "tcp", port: "1881" },
        { name: "tcp", port: "502" },
        { name: "tcp", port: "20000" },
        { name: "udp", port: "123" },
      ],
      ics: {},
      action: "ALLOW",
    },
    {
      id: "gps-to-field-ntp",
      description: "GPS to Field: NTP time sync",
      sourceZones: ["lan1"],
      destZones: ["lan2"],
      sources: ["10.30.30.50/32"],
      protocols: [{ name: "udp", port: "123" }],
      ics: {},
      action: "ALLOW",
    },
    {
      id: "allow-return-traffic",
      description: "Allow established return traffic",
      state: ["ESTABLISHED", "RELATED"],
      ics: {},
      action: "ALLOW",
    }
  );

  return {
    schema_version: "0.1.0",
    system: {
      hostname: "containd",
      mgmt: {
        listenAddr: ":8080",
        enableHTTP: true,
        enableHTTPS: true,
        httpListenAddr: ":8080",
        httpsListenAddr: ":8443",
        tlsCertFile: "/data/tls/server.crt",
        tlsKeyFile: "/data/tls/server.key",
      },
      ssh: {},
    },
    interfaces: [
      { name: "enterprise", device: "eth0", zone: "wan", access: {} },
      { name: "vendor", device: "eth1", zone: "dmz", access: {} },
      { name: "ot_ops", device: "eth2", zone: "lan1", access: {} },
      { name: "field_devices", device: "eth3", zone: "lan2", access: {} },
    ],
    zones: [
      { name: "wan", description: "Enterprise Zone" },
      { name: "dmz", description: "Vendor / Engineering Zone" },
      { name: "lan1", description: "OT Operations Zone" },
      { name: "lan2", description: "Field Device Zone" },
    ],
    routing: {},
    dataplane: {},
    pcap: { filter: {} },
    firewall: { defaultAction: "DENY", rules, nat: { enabled: false } },
    ids: { enabled: false },
    services: {
      syslog: { forwarders: [], format: "rfc5424", batchSize: 500, flushEvery: 2 },
      dns: { enabled: false },
      ntp: { enabled: false },
      proxy: { forward: { enabled: false }, reverse: { enabled: false } },
      dhcp: { enabled: false },
      vpn: { wireguard: { enabled: false }, openvpn: { enabled: false } },
      av: { enabled: false, icap: {}, clamav: {} },
    },
  };
}
