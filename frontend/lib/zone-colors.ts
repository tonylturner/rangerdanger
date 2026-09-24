// Zone colors - substation segmentation lab + legacy zone names
export const zoneColors: Record<string, { border: string; bg: string; text: string }> = {
  // Substation segmentation zones
  enterprise_net: { border: "#38bdf8", bg: "rgba(56, 189, 248, 0.1)", text: "#38bdf8" },  // sky blue
  vendor_net:     { border: "#a855f7", bg: "rgba(168, 85, 247, 0.1)", text: "#a855f7" },  // purple
  ot_ops_net:     { border: "#f97316", bg: "rgba(249, 115, 22, 0.1)", text: "#f97316" },  // orange
  field_net:      { border: "#22c55e", bg: "rgba(34, 197, 94, 0.1)", text: "#22c55e" },   // green
  // Containd zone names (from firewall config)
  wan:        { border: "#38bdf8", bg: "rgba(56, 189, 248, 0.1)", text: "#38bdf8" },
  dmz:        { border: "#a855f7", bg: "rgba(168, 85, 247, 0.1)", text: "#a855f7" },
  lan1:       { border: "#f97316", bg: "rgba(249, 115, 22, 0.1)", text: "#f97316" },
  lan2:       { border: "#22c55e", bg: "rgba(34, 197, 94, 0.1)", text: "#22c55e" },
  ot_control: { border: "#f97316", bg: "rgba(249, 115, 22, 0.1)", text: "#f97316" },
  ot_safety:  { border: "#22c55e", bg: "rgba(34, 197, 94, 0.1)", text: "#22c55e" },
};
