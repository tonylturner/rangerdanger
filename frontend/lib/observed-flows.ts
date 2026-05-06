// Shared model of the lab's observable host-to-host flows.
//
// Both the Network Map's Traffic view (canvas edges) and the Traffic
// Matrix drawer pull from this single table so the two presentations
// stay in lockstep — and so generation of new flows always goes
// through the same backend path (`/api/traffic/generate`, which
// docker-execs real curl inside eng-ws and vendor-jump). The matrix
// is no longer "what the supervisory audit log captured" — it's "what
// the lab is configured to produce, and which of those flows is
// currently live".
//
// Each entry has a `livenessKey` consulted at render time by
// resolveLiveness() to map it to a runtime telemetry source:
//
//   - rtac-relay / rtac-recloser / rtac-regulator → RTAC device_comms
//   - rtac → RTAC online state (covers HMI/historian polling RTAC)
//   - gps → RTAC online (NTP broadcast piggybacks on the OT mesh)
//   - scenario → traffic generator state (active during generation
//     plus a 60s tail so the user can see a recent burst)

import type { TrafficStatus } from "./api";

export type FlowCategory = "baseline" | "scenario";
export type FlowStatus = "active" | "idle" | "down";

export type ObservedFlow = {
  source: string;       // workshop graph node id (e.g. "rtac-1")
  target: string;
  protocol: string;     // human label ("Modbus", "DNP3", "HTTP", ...)
  port: number;
  cadence: string;      // human label ("~3s", "burst")
  category: FlowCategory;
  livenessKey: string;
};

export const OBSERVED_FLOWS: ObservedFlow[] = [
  // ── Autonomous baseline ──────────────────────────────────────────
  { source: "rtac-1", target: "relay-1",      protocol: "Modbus", port: 502,   cadence: "~3s",  category: "baseline", livenessKey: "rtac-relay" },
  { source: "rtac-1", target: "recloser-1",   protocol: "Modbus", port: 502,   cadence: "~3s",  category: "baseline", livenessKey: "rtac-recloser" },
  { source: "rtac-1", target: "regulator-1",  protocol: "Modbus", port: 502,   cadence: "~3s",  category: "baseline", livenessKey: "rtac-regulator" },
  { source: "rtac-1", target: "relay-1",      protocol: "DNP3",   port: 20000, cadence: "~5s",  category: "baseline", livenessKey: "rtac-relay" },
  { source: "rtac-1", target: "recloser-1",   protocol: "DNP3",   port: 20000, cadence: "~5s",  category: "baseline", livenessKey: "rtac-recloser" },
  { source: "rtac-1", target: "regulator-1",  protocol: "DNP3",   port: 20000, cadence: "~5s",  category: "baseline", livenessKey: "rtac-regulator" },
  { source: "gps-1",  target: "relay-1",      protocol: "NTP",    port: 123,   cadence: "~30s", category: "baseline", livenessKey: "gps" },
  { source: "gps-1",  target: "recloser-1",   protocol: "NTP",    port: 123,   cadence: "~30s", category: "baseline", livenessKey: "gps" },
  { source: "gps-1",  target: "regulator-1",  protocol: "NTP",    port: 123,   cadence: "~30s", category: "baseline", livenessKey: "gps" },
  { source: "rtac-1", target: "capbank-1",    protocol: "Modbus", port: 502,   cadence: "~3s",  category: "baseline", livenessKey: "rtac-capbank" },
  { source: "rtac-1", target: "capbank-1",    protocol: "DNP3",   port: 20000, cadence: "~5s",  category: "baseline", livenessKey: "rtac-capbank" },
  { source: "gps-1",  target: "capbank-1",    protocol: "NTP",    port: 123,   cadence: "~30s", category: "baseline", livenessKey: "gps" },
  { source: "hmi-1",  target: "rtac-1",       protocol: "Modbus", port: 502,   cadence: "~2s",  category: "baseline", livenessKey: "rtac" },
  { source: "historian-1", target: "rtac-1", protocol: "HTTP",    port: 8080,  cadence: "~5s",  category: "baseline", livenessKey: "rtac" },

  // ── Scenario-driven (only while traffic generator is running) ───

  // Engineering workstation (vendor zone) → OT ops
  { source: "eng-ws-1",      target: "rtac-1",       protocol: "HTTP",   port: 8080,  cadence: "burst", category: "scenario", livenessKey: "scenario" },
  { source: "eng-ws-1",      target: "openplc-1",    protocol: "HTTP",   port: 8080,  cadence: "burst", category: "scenario", livenessKey: "scenario" },
  { source: "eng-ws-1",      target: "hmi-1",        protocol: "FUXA",   port: 1881,  cadence: "burst", category: "scenario", livenessKey: "scenario" },

  // Engineering workstation → field devices (WEAK: blocked in hardened)
  // Uses real ICS protocol tools: mbpoll for Modbus FC03 reads,
  // dnp3poll for DNP3 class-0 polls, plus HTTP REST reads. Students
  // capture these with tshark and see proper Modbus/DNP3 frames.
  // Engineering workstation → field devices: real ICS protocols only.
  // Modbus FC03 reads via mbpoll + DNP3 class-0 polls via dnp3poll.
  // No HTTP/8080 — real relays, reclosers, regulators, and cap banks
  // don't run web servers. The HTTP API on port 8080 is a lab
  // convenience for Go simulators, not a realistic field protocol.
  { source: "eng-ws-1",      target: "relay-1",      protocol: "Modbus", port: 502,   cadence: "burst", category: "scenario", livenessKey: "scenario" },
  { source: "eng-ws-1",      target: "recloser-1",   protocol: "Modbus", port: 502,   cadence: "burst", category: "scenario", livenessKey: "scenario" },
  { source: "eng-ws-1",      target: "regulator-1",  protocol: "Modbus", port: 502,   cadence: "burst", category: "scenario", livenessKey: "scenario" },
  { source: "eng-ws-1",      target: "capbank-1",    protocol: "Modbus", port: 502,   cadence: "burst", category: "scenario", livenessKey: "scenario" },
  { source: "eng-ws-1",      target: "relay-1",      protocol: "DNP3",   port: 20000, cadence: "burst", category: "scenario", livenessKey: "scenario" },
  { source: "eng-ws-1",      target: "recloser-1",   protocol: "DNP3",   port: 20000, cadence: "burst", category: "scenario", livenessKey: "scenario" },
  { source: "eng-ws-1",      target: "regulator-1",  protocol: "DNP3",   port: 20000, cadence: "burst", category: "scenario", livenessKey: "scenario" },
  { source: "eng-ws-1",      target: "capbank-1",    protocol: "DNP3",   port: 20000, cadence: "burst", category: "scenario", livenessKey: "scenario" },

  // Vendor jump box (vendor zone) → OT ops
  { source: "vendor-jump-1", target: "hmi-1",        protocol: "FUXA",   port: 1881,  cadence: "burst", category: "scenario", livenessKey: "scenario" },
  { source: "vendor-jump-1", target: "rtac-1",       protocol: "HTTP",   port: 8080,  cadence: "burst", category: "scenario", livenessKey: "scenario" },
];

export function resolveLiveness(
  flow: ObservedFlow,
  deviceComms: Record<string, boolean> | undefined,
  rtacOnline: boolean,
  trafficStatus: TrafficStatus | undefined,
): FlowStatus {
  switch (flow.livenessKey) {
    case "rtac-relay":
    case "rtac-recloser":
    case "rtac-regulator":
    case "rtac-capbank": {
      const dev = flow.livenessKey.split("-")[1];
      if (!rtacOnline) return "down";
      const ok = deviceComms?.[dev];
      return ok ? "active" : "down";
    }
    case "rtac":
    case "gps":
      return rtacOnline ? "active" : "down";
    case "scenario": {
      if (trafficStatus?.generating) return "active";
      if (trafficStatus?.started_at) {
        const startedMs = new Date(trafficStatus.started_at).getTime();
        if (!Number.isNaN(startedMs) && Date.now() - startedMs < 60_000) {
          return "active";
        }
      }
      return "idle";
    }
    default:
      return "idle";
  }
}

// Lab IP/zone metadata used by both the matrix view and the canvas
// inspector. Mirrors the substation lab definition.
export const NODE_ZONE: Record<string, "enterprise" | "vendor" | "ot_ops" | "field"> = {
  "corp-ws-1": "enterprise",
  "kali-1": "enterprise",
  "vendor-jump-1": "vendor",
  "eng-ws-1": "vendor",
  "hmi-1": "ot_ops",
  "rtac-1": "ot_ops",
  "openplc-1": "ot_ops",
  "historian-1": "ot_ops",
  "gps-1": "ot_ops",
  "relay-1": "field",
  "recloser-1": "field",
  "regulator-1": "field",
  "capbank-1": "field",
};

export function nodeZone(nodeId: string): string {
  return NODE_ZONE[nodeId] || "unknown";
}

export function isCrossZone(source: string, target: string): boolean {
  const a = nodeZone(source);
  const b = nodeZone(target);
  return a !== b && a !== "unknown" && b !== "unknown";
}

// Static node→IP map mirroring the substation lab's docker-compose.
// Used for hover tooltips so students can correlate the matrix to
// addresses they'd see in tcpdump or tshark.
export const NODE_IP: Record<string, string> = {
  "corp-ws-1": "10.10.10.10",
  "kali-1": "10.10.10.50",
  "vendor-jump-1": "10.20.20.10",
  "eng-ws-1": "10.20.20.20",
  "hmi-1": "10.30.30.10",
  "rtac-1": "10.30.30.20",
  "openplc-1": "10.30.30.30",
  "historian-1": "10.30.30.40",
  "gps-1": "10.30.30.50",
  "relay-1": "10.40.40.20",
  "recloser-1": "10.40.40.21",
  "regulator-1": "10.40.40.22",
  "capbank-1": "10.40.40.23",
};

export function nodeIp(nodeId: string): string | undefined {
  return NODE_IP[nodeId];
}
