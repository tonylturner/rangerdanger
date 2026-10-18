import type { ObservedFlow } from "./observed-flows";

export const CONNECTION_DIRECTION = "Arrows show who initiates the connection, not information direction. IEC 104 and Modbus exchanges are bidirectional.";
export const PORT_RULE_BOUNDARY = "Port rules do not distinguish reads from writes; command-level filtering would need deep packet inspection (DPI).";
export const STATION_TIME_BOUNDARY = "Station time is simulated over HTTP, not NTP/PTP.";

// Teaching purposes supplement the declared inventory; they are not evidence
// that a session succeeded or a firewall rule authorized a command.
export function flowPurpose(flow: ObservedFlow): string {
  switch (flow.id) {
    case "cc-rtu":
      return "Monitoring and control — CC initiates IEC 104 to the RTU; observations return on the same session that carries supervisory commands.";
    case "rtu-breaker":
    case "rtu-rmu-1":
    case "rtu-rmu-2":
    case "rtu-oltc-1":
      return "Monitoring and control — RTU polls field indications and relays supervisory requests over Modbus.";
    case "rtu-plc-1":
      return "Monitoring — RTU polls PLC regulation status over Modbus. RTU→PLC stays inside the station zone and does not traverse containd.";
    case "station-clock":
      return `Time quality — RTU→clock stays inside the station zone and does not traverse containd. ${STATION_TIME_BOUNDARY}`;
    case "plc-oltc":
      return "Regulation — PLC initiates Modbus to the OLTC, reads feedback and writes tap requests in its autonomous voltage-regulation loop.";
    case "cc-historian":
      return "Archive ingestion — CC pushes received-observation snapshots to the historian; the historian does not poll field truth.";
    case "enterprise-historian":
      return "Archive reading — enterprise client reads historian observations, not the separate snapshot-ingest listener.";
    case "engineering-plc":
      return "Engineering — engineering workstation initiates PLC web access through the teaching conduit. Portal OpenPLC access is separate management access.";
    case "attacker-rtu":
      return "Exercise traffic — enterprise client attempts IEC 104 access to the RTU; the declared path does not authorize monitoring or control.";
    default:
      return `${flow.category === "scenario" ? "Exercise" : "Operational"} traffic — ${flow.protocol} exchange declared by this package.`;
  }
}
