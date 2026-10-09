// Readable names for topology node types that would otherwise show as
// their raw identifier in the network map. Types without an entry
// keep their raw identifier, as the US package's types always have.
export const NODE_TYPE_LABELS: Record<string, string> = {
  control_centre: "Control centre (IEC 104 controlling station)",
  iec104_rtu: "IEC 104 RTU (controlled station)",
  feeder_breaker: "Feeder breaker (Modbus field device)",
};

export function nodeTypeLabel(type: string): string {
  return NODE_TYPE_LABELS[type] ?? type;
}
