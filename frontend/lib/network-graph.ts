import type { Edge, Node } from "reactflow";
import type { GraphNode as ApiGraphNode, LabGraph, TrafficStatus, ZoneRuleSummary } from "./api";
import { OBSERVED_FLOWS, isCrossZone, nodeZone, resolveLiveness, type ObservedFlow } from "./observed-flows";
import {
  ViewMode,
  deriveNodeHealth,
  getFlowBlockReason,
  getZonePolicyInfo,
  zones,
  ZONE_INTERFACE_META,
} from "./network-console-data";
import { zoneColors } from "./zone-colors";

export function buildStyledGraph(
  graph?: LabGraph,
  ruleSummaries?: ZoneRuleSummary[],
  viewMode: ViewMode = { policyDim: true, traffic: false, iec62443: false },
  deviceComms?: Record<string, boolean>,
  rtacOnline?: boolean,
  trafficStatus?: TrafficStatus,
  firewallOnline?: boolean,
  trafficFilter?: { zone: string; crossZoneOnly: boolean },
  highlightedTrafficPair?: string | null,
) {
    if (!graph) return { nodes: [] as Node[], edges: [] as Edge[] };

    const workshopOnline = {
      firewall: firewallOnline,
      rtac: rtacOnline,
      deviceComms,
    };

    // Layout configuration
    const ZONE_SPACING_X = 240;
    const NODE_SPACING_Y = 130;
    const FIREWALL_Y = 50;
    // Pushed down from 220 to give the firewall icon, its label,
    // and the zone boundary headers room to breathe vertically.
    const ZONE_START_Y = 290;

    // Find the firewall node (containd_ngfw or opnsense_external)
    const firewallNode = graph.nodes.find((n) =>
      n.type === "containd_ngfw" || n.type === "opnsense_external"
    );

    // Group non-firewall, non-zone nodes by zone (host nodes only)
    const nodesByZone: Record<string, ApiGraphNode[]> = {};
    graph.nodes.forEach((n) => {
      if (n.type === "containd_ngfw" || n.type === "opnsense_external" || n.type === "zone") {
        return;
      }
      const zone = n.data.zone || "unknown";
      if (!nodesByZone[zone]) nodesByZone[zone] = [];
      nodesByZone[zone].push(n);
    });

    // Define preferred zone order for layout
    const zoneOrder = [
      "enterprise_net", "wan", "it_net",
      "vendor_net", "dmz", "dmz_net",
      "ot_ops_net", "lan1", "ot_control", "ot_control_net",
      "field_net", "lan2", "ot_safety", "ot_safety_net",
    ];
    const activeZones = Object.keys(nodesByZone)
      .filter(z => zones.includes(z as typeof zones[number]))
      .sort((a, b) => {
        const aIdx = zoneOrder.indexOf(a);
        const bIdx = zoneOrder.indexOf(b);
        return (aIdx === -1 ? 999 : aIdx) - (bIdx === -1 ? 999 : bIdx);
      });

    // ── Purdue Model band layout ──────────────────────────────────
    // Horizontal architectural bands stacked top-to-bottom, mapping
    // lab zones to Purdue levels (L4 → L3.5 → L3 → L1). The firewall
    // sits on the L3.5 IDMZ boundary. This is *Purdue* architecture,
    // not IEC 62443 - IEC 62443 contributes the Security Level (SL)
    // overlay rendered alongside each conduit. Both frameworks are in
    // play here, but the *band layout* is Purdue-driven.
    if (viewMode.iec62443) {
      // Horizontal Purdue-level bands stacked top-to-bottom, each
      // showing the IEC 62443 Security Level (SL) it should achieve.
      // The firewall/conduit sits to the RIGHT of the bands so the
      // student can evaluate whether the conduit's SL meets the target
      // SL (= max of the two connected zones).
      // all connected zone SLs). Conduit lines run horizontally
      // from each band's right edge to the firewall.
      //
      // In the weak baseline the conduit is SL 1 (pass-through),
      // which fails the SL 3 target set by OT Operations. In the
      // hardened config it's SL 3 (deny-default, DPI, source-pin).

      const BAND_WIDTH = 700;
      const BAND_HEIGHT = 180;
      const BAND_GAP = 30;
      const BAND_START_X = -400;
      const NODE_START_X_OFFSET = 30;
      const NODE_SPACING_X = 130;
      const FW_X = BAND_START_X + BAND_WIDTH + 120; // firewall to the right of bands

      // IEC 62443 Security Level assignments for each zone.
      // SL 0 = none, SL 1 = casual, SL 2 = intentional/simple,
      // SL 3 = sophisticated, SL 4 = state-sponsored.
      const ZONE_SL: Record<string, number> = {
        enterprise_net: 1, // IT-managed, casual threat model
        vendor_net: 2,     // authenticated remote access
        ot_ops_net: 3,     // critical SCADA, sophisticated threats
        field_net: 2,      // limited compute, physical hardening
      };

      // Purdue level mapping - ALL levels top to bottom (firewall is NOT between them)
      const purdueOrder: Array<{ zone: string; label: string }> = [
        { zone: "enterprise_net", label: "Level 4 \u00B7 Enterprise" },
        { zone: "vendor_net", label: "Level 3.5 \u00B7 DMZ" },
        { zone: "ot_ops_net", label: "Level 3 \u00B7 Supervisory" },
        { zone: "field_net", label: "Level 1 \u00B7 Field Devices" },
      ];
      const activeLevels = purdueOrder.filter((p) => activeZones.includes(p.zone));

      const zoneSubnets62443: Record<string, string> = {
        enterprise_net: "10.10.10.0/24",
        vendor_net: "10.20.20.0/24",
        ot_ops_net: "10.30.30.0/24",
        field_net: "10.40.40.0/24",
      };

      const styledNodes: Node[] = [];
      let curY = 0;

      // Render all Purdue bands sequentially
      for (const level of activeLevels) {
        const sl = ZONE_SL[level.zone] ?? 0;

        styledNodes.push({
          id: `zone-boundary-${level.zone}`,
          type: "zoneBoundary",
          position: { x: BAND_START_X, y: curY },
          data: {
            zone: level.zone,
            label: `${level.label}  \u00B7  SL-${sl}`,
            subnet: zoneSubnets62443[level.zone],
            width: BAND_WIDTH,
            height: BAND_HEIGHT,
            securityLevel: sl,
          },
          selectable: false,
          draggable: false,
          zIndex: 0,
        });

        // Zone anchor at the RIGHT EDGE of the band, vertically
        // centered - conduit lines go from here to the firewall.
        styledNodes.push({
          id: `zone-${level.zone}`,
          type: "zone",
          position: {
            x: BAND_START_X + BAND_WIDTH,
            y: curY + BAND_HEIGHT / 2,
          },
          data: { label: "", zone: level.zone, subnet: undefined },
          selectable: false,
          draggable: false,
          zIndex: 1,
        });

        // Host nodes inside the band
        const zoneNodes = nodesByZone[level.zone] || [];
        zoneNodes.forEach((n, nodeIdx) => {
          const allInterfaceZones = n.data.interface_ips
            ? Object.keys(n.data.interface_ips).filter((z) => zones.includes(z as typeof zones[number]))
            : (n.data.networks || []);
          const additionalZones = allInterfaceZones.filter((z) => z !== level.zone);
          const nodeHealth = deriveNodeHealth(n.id, workshopOnline);

          styledNodes.push({
            id: n.id,
            type: "host",
            position: {
              x: BAND_START_X + NODE_START_X_OFFSET + nodeIdx * NODE_SPACING_X,
              y: curY + 50,
            },
            data: {
              label: n.data.label || n.id,
              nodeType: n.type,
              zone: level.zone,
              status: n.data.status || "running",
              ip: n.data.ip,
              interface_ips: n.data.interface_ips,
              networks: n.data.networks,
              ui_path: n.data.ui_path,
              external_ui_url: n.data.external_ui_url,
              multiHomedZones: additionalZones,
              health: nodeHealth.health,
              healthSource: nodeHealth.healthSource,
              verticalOnly: true,
            },
            draggable: true,
          });
        });

        curY += BAND_HEIGHT + BAND_GAP;
      }

      // Conduit Security Level assessment. The conduit (firewall)
      // must meet or exceed the highest SL of any zone it connects.
      // In the weak baseline it's SL 1 (pass-through, no DPI);
      // in the hardened config it's SL 3 (deny-default, source-pin,
      // protocol-aware DPI). This drives the green/red indicator
      // on the firewall label.
      const targetSL = Math.max(...activeLevels.map((l) => ZONE_SL[l.zone] ?? 0));
      const isHardened = firewallOnline !== undefined
        ? (ruleSummaries || []).every((r) =>
            !r.rule_details.some((d) => d.trim().toUpperCase().startsWith("WEAK:")),
          )
        : false;
      const conduitSL = isHardened ? 3 : 1;
      const conduitMet = conduitSL >= targetSL;

      // Firewall positioned to the RIGHT of all bands, vertically
      // centered, so conduit lines run horizontally from each band.
      if (firewallNode) {
        const fwHealth = deriveNodeHealth(firewallNode.id, workshopOnline);
        const fwCenterY = (curY - BAND_GAP) / 2 - 50; // vertically center among all bands
        styledNodes.push({
          id: firewallNode.id,
          type: "firewall",
          position: { x: FW_X, y: fwCenterY },
          data: {
            label: `Conduit \u00B7 SL-${conduitSL}`,
            nodeType: firewallNode.type,
            zone: "enterprise_net",
            status: firewallNode.data.status || "running",
            ip: firewallNode.data.ip,
            interface_ips: firewallNode.data.interface_ips,
            networks: firewallNode.data.networks,
            ui_path: firewallNode.data.ui_path,
            external_ui_url: firewallNode.data.external_ui_url,
            health: fwHealth.health,
            healthSource: fwHealth.healthSource,
            // Extra 62443 fields for the label area
            conduitSL,
            targetSL,
            conduitMet,
          },
          draggable: true,
        });
      }

      // Conduit edges - in the 62443 view these are ALWAYS solid
      // zone-colored lines with a glow. The policy state (allowed /
      // over-permissive / blocked) is NOT encoded on individual
      // conduit lines because the 62443 view expresses policy
      // compliance as the SL assessment on the firewall/conduit
      // node itself. Edges run from the zone's RIGHT handle to the
      // firewall's LEFT handle so they converge on one side.
      const styledEdges: Edge[] = [];
      if (firewallNode) {
        activeZones.forEach((zone) => {
          const zoneStroke = zoneColors[zone]?.border || "#64748b";
          const policyInfo = getZonePolicyInfo(zone, ruleSummaries);

          styledEdges.push({
            id: `fw-to-${zone}`,
            source: `zone-${zone}`,
            sourceHandle: "right",
            target: firewallNode.id,
            targetHandle: "left",
            type: "policyEdge",
            style: {
              stroke: zoneStroke,
              strokeWidth: 2.4,
              opacity: 1,
              filter: `drop-shadow(0 0 5px ${zoneStroke})`,
            },
            animated: false,
            data: {
              label: policyInfo.label || "",
              details: policyInfo.details,
              action: policyInfo.action,
              permissiveness: policyInfo.permissiveness,
              color: zoneStroke,
              dimmed: false,
              pulse: false,
              zoneMeta: ZONE_INTERFACE_META[zone],
            },
          });
        });
      }

      // Traffic edges - same logic as default layout
      if (viewMode.traffic) {
        const presentNodeIds = new Set(styledNodes.map((n) => n.id));
        const nodeIndex = new Map(styledNodes.map((n) => [n.id, n]));

        const groups = new Map<string, ObservedFlow[]>();
        for (const flow of OBSERVED_FLOWS) {
          if (!presentNodeIds.has(flow.source) || !presentNodeIds.has(flow.target)) continue;
          const key = `${flow.source}->${flow.target}`;
          if (!groups.has(key)) groups.set(key, []);
          groups.get(key)!.push(flow);
        }

        const fZone = trafficFilter?.zone ?? "all";
        const fCrossOnly = trafficFilter?.crossZoneOnly ?? false;
        const anyHighlight = !!highlightedTrafficPair;

        Array.from(groups.entries()).forEach(([key, flows]) => {
          const first = flows[0];
          const sZone = nodeZone(first.source);
          const tZone = nodeZone(first.target);
          const crossZone = isCrossZone(first.source, first.target);

          if (fCrossOnly && !crossZone) return;
          if (fZone !== "all" && sZone !== fZone && tZone !== fZone) return;

          const statuses = flows.map((f) =>
            resolveLiveness(f, deviceComms, rtacOnline ?? false, trafficStatus),
          );
          const status: "active" | "idle" | "down" = statuses.includes("active")
            ? "active"
            : statuses.every((s) => s === "down")
            ? "down"
            : "idle";

          const protocols: string[] = [];
          for (const f of flows) {
            if (!protocols.includes(f.protocol)) protocols.push(f.protocol);
          }
          const label =
            protocols.length <= 3
              ? protocols.join(" + ")
              : `${protocols.slice(0, 2).join(" + ")} +${protocols.length - 2}`;

          // No sourceHandle/targetHandle - HostNodes have exactly one
          // source (right) and one target (left) so React Flow picks
          // them automatically.

          const tooltipLines = [
            `${first.source} \u2192 ${first.target}`,
            ...flows.map((f) => `${f.protocol} on tcp/${f.port} \u00B7 ${f.cadence}`),
            `category: ${first.category}`,
          ];

          const isHighlighted = highlightedTrafficPair === key;
          const isDimmedByHighlight = anyHighlight && !isHighlighted;
          // Only check firewall blocking when Policy view is active -
          // the red ✕ is a policy concept and shouldn't show in pure
          // topology mode.
          const blockReason = viewMode.policyDim
            ? getFlowBlockReason(first.source, first.target, ruleSummaries)
            : null;
          const blockedByFw = blockReason !== null;

          styledEdges.push({
            id: `traffic-${key}`,
            source: first.source,
            target: first.target,
            type: "trafficEdge",
            animated: status === "active" && !isDimmedByHighlight && !blockedByFw,
            data: {
              label,
              status,
              category: first.category,
              highlighted: isHighlighted,
              dimmedByHighlight: isDimmedByHighlight,
              blockedByFirewall: blockedByFw,
              blockReason: blockReason,
              tooltipLines,
            },
          });
        });
      }

      return { nodes: styledNodes, edges: styledEdges };
    }

    // ── Default vertical-column layout ───────────────────────────

    // Calculate layout dimensions
    const numZones = activeZones.length;
    const totalWidth = (numZones - 1) * ZONE_SPACING_X;
    const startX = -totalWidth / 2;
    const centerX = 0;

    const styledNodes: Node[] = [];

    // Add firewall node at top center
    if (firewallNode) {
      const fwHealth = deriveNodeHealth(firewallNode.id, workshopOnline);
      styledNodes.push({
        id: firewallNode.id,
        type: "firewall",
        // Firewall wrapper is 160px wide; shift -80 so the icon
        // centers on the canvas centerline.
        position: { x: centerX - 80, y: FIREWALL_Y },
        data: {
          label: firewallNode.data.label || "containd NGFW",
          nodeType: firewallNode.type,
          zone: "enterprise_net",
          status: firewallNode.data.status || "running",
          ip: firewallNode.data.ip,
          interface_ips: firewallNode.data.interface_ips,
          networks: firewallNode.data.networks,
          ui_path: firewallNode.data.ui_path,
          external_ui_url: firewallNode.data.external_ui_url,
          health: fwHealth.health,
          healthSource: fwHealth.healthSource,
        },
        draggable: true,
      });
    }

    // Zone display labels and subnets
    const zoneLabels: Record<string, string> = {
      enterprise_net: "Enterprise Zone",
      vendor_net: "Vendor / Engineering",
      ot_ops_net: "OT Operations",
      field_net: "Field Devices",
      wan: "Enterprise Zone",
      dmz: "Vendor / Engineering",
      ot_control: "OT Operations",
      ot_safety: "Field Devices",
      it_workstations: "IT Workstations",
      it_net: "Enterprise Zone",
      dmz_net: "Vendor / Engineering",
      ot_control_net: "OT Operations",
      ot_safety_net: "Field Devices",
    };

    const zoneSubnets: Record<string, string> = {
      enterprise_net: "10.10.10.0/24",
      vendor_net: "10.20.20.0/24",
      ot_ops_net: "10.30.30.0/24",
      field_net: "10.40.40.0/24",
      wan: "10.10.10.0/24",
      dmz: "10.20.20.0/24",
      ot_control: "10.30.30.0/24",
      ot_safety: "10.40.40.0/24",
      it_net: "10.10.10.0/24",
      dmz_net: "10.20.20.0/24",
      ot_control_net: "10.30.30.0/24",
      ot_safety_net: "10.40.40.0/24",
    };

    // Boundary box geometry shared across all zones. The boundary
    // wraps a zone column with enough room to clear the icons + the
    // host labels below them.
    const BOUNDARY_HALF_WIDTH = 100;
    const BOUNDARY_TOP_PAD = 36;
    // The host wrapper at `lastNodeY` is the icon (56) + margin
    // (8) + label (up to ~32 if it wraps) + IP (14) ≈ 110px tall.
    // We need that plus a comfortable cushion below the IP so the
    // metadata never bleeds past the boundary's bottom edge.
    const BOUNDARY_BOTTOM_PAD = 140;
    const BOUNDARY_TOP_Y = ZONE_START_Y - BOUNDARY_TOP_PAD;

    // Add zone BOUNDARY nodes first so they render BEHIND the hosts.
    // Each boundary is a translucent rounded panel that visually
    // groups the hosts inside the zone - the segmentation grouping
    // becomes immediately legible without any heavy borders.
    activeZones.forEach((zone, zoneIdx) => {
      const zoneX = startX + zoneIdx * ZONE_SPACING_X;
      const zoneNodes = nodesByZone[zone] || [];
      const lastNodeY = ZONE_START_Y + 100 + Math.max(0, zoneNodes.length - 1) * NODE_SPACING_Y;
      const boundaryHeight = lastNodeY + BOUNDARY_BOTTOM_PAD - BOUNDARY_TOP_Y;
      styledNodes.push({
        id: `zone-boundary-${zone}`,
        type: "zoneBoundary",
        position: {
          x: zoneX - BOUNDARY_HALF_WIDTH,
          y: BOUNDARY_TOP_Y,
        },
        data: {
          zone,
          label: zoneLabels[zone] || zone.toUpperCase(),
          subnet: zoneSubnets[zone],
          width: BOUNDARY_HALF_WIDTH * 2,
          height: boundaryHeight,
        },
        selectable: false,
        draggable: false,
        zIndex: 0,
      });
    });

    // Add zone nodes and host nodes
    activeZones.forEach((zone, zoneIdx) => {
      const zoneX = startX + zoneIdx * ZONE_SPACING_X;
      const zoneNodes = nodesByZone[zone] || [];

      // Tiny invisible anchor node sitting EXACTLY on the top edge of
      // the zone boundary panel. Firewall→zone conduits terminate
      // here so they visually end at the zone border (not floating
      // in dead space inside the panel and not piercing through to
      // the devices). Reinforces the segmentation model: the
      // conduit stops at the policy enforcement boundary.
      styledNodes.push({
        id: `zone-${zone}`,
        type: "zone",
        position: { x: zoneX, y: BOUNDARY_TOP_Y },
        data: {
          label: "",
          zone,
          subnet: undefined,
        },
        selectable: false,
        draggable: false,
        zIndex: 1,
      });

      // Add host nodes below the zone label
      zoneNodes.forEach((n, nodeIdx) => {
        // Detect multi-homing: a node with interfaces in more than one
        // zone gets a "MULTI" badge and a list of additional zones for
        // the inspector. The primary zone is the one we placed it in.
        const allInterfaceZones = n.data.interface_ips
          ? Object.keys(n.data.interface_ips).filter((z) => zones.includes(z as typeof zones[number]))
          : (n.data.networks || []);
        const additionalZones = allInterfaceZones.filter((z) => z !== zone);

        const nodeHealth = deriveNodeHealth(n.id, workshopOnline);

        // React Flow positions are top-left of the node wrapper.
        // Host wrappers are 120px wide (minWidth) so we shift by -60
        // to put the visible icon right on the zone column center.
        styledNodes.push({
          id: n.id,
          type: "host",
          position: {
            x: zoneX - 60,
            y: ZONE_START_Y + 100 + nodeIdx * NODE_SPACING_Y,
          },
          data: {
            label: n.data.label || n.id,
            nodeType: n.type,
            zone: zone,
            status: n.data.status || "running",
            ip: n.data.ip,
            interface_ips: n.data.interface_ips,
            networks: n.data.networks,
            ui_path: n.data.ui_path,
            external_ui_url: n.data.external_ui_url,
            multiHomedZones: additionalZones,
            health: nodeHealth.health,
            healthSource: nodeHealth.healthSource,
          },
          draggable: true,
        });
      });
    });

    // Create edges
    const styledEdges: Edge[] = [];

    // Edges from firewall to zones - segmentation boundaries.
    //
    // Policy view ON: edges are colored by action (zone color for
    // ALLOW, red for DENY, amber for MIXED), allowed paths are
    // animated, denied paths are dashed and dimmed. The student can
    // scan policy state at a glance.
    //
    // Policy view OFF: every edge looks the same - neutral grey, no
    // animation, no action color, no protocol label. The map becomes
    // a pure topology view ("what exists"), independent of policy
    // ("what's allowed"). This makes the toggle visibly do something
    // even when the current policy has no DENY rules.
    if (firewallNode) {
      activeZones.forEach((zone) => {
        const zoneStroke = zoneColors[zone]?.border || "#64748b";
        const policyInfo = getZonePolicyInfo(zone, ruleSummaries);
        const policyOn = viewMode.policyDim;

        // State encoding driven by `permissiveness`:
        //   allowed → steady cyan/zone glow
        //   over    → thicker amber/orange + soft pulse (warning)
        //   blocked → dim dashed red
        //   unknown → faint grey
        // Policy view OFF flattens everything to neutral grey.
        const state = policyOn ? policyInfo.permissiveness : "topology";
        const styleByState: Record<
          string,
          { stroke: string; width: number; dash?: string; opacity: number; glow: number }
        > = {
          allowed: { stroke: zoneStroke, width: 2.4, opacity: 1, glow: 5 },
          over: { stroke: "#f59e0b", width: 3.4, opacity: 1, glow: 8 },
          blocked: { stroke: "#ef4444", width: 1.6, dash: "6 4", opacity: 0.55, glow: 3 },
          unknown: { stroke: "#64748b", width: 1.4, opacity: 0.7, glow: 0 },
          topology: { stroke: "#475569", width: 1.4, opacity: 0.85, glow: 0 },
        };
        const cfg = styleByState[state];

        styledEdges.push({
          id: `fw-to-${zone}`,
          source: firewallNode.id,
          target: `zone-${zone}`,
          type: "policyEdge",
          style: {
            stroke: cfg.stroke,
            strokeWidth: cfg.width,
            strokeDasharray: cfg.dash,
            opacity: cfg.opacity,
            filter: cfg.glow ? `drop-shadow(0 0 ${cfg.glow}px ${cfg.stroke})` : undefined,
          },
          // Allowed paths animate (energized conduit). Over-permissive
          // edges get the CSS pulse class via data.pulse so a student
          // catches the warning at a glance.
          animated: state === "allowed",
          data: {
            label: policyOn ? policyInfo.label : "",
            details: policyInfo.details,
            action: policyInfo.action,
            permissiveness: policyInfo.permissiveness,
            color: cfg.stroke,
            dimmed: state === "blocked",
            pulse: state === "over",
            zoneMeta: ZONE_INTERFACE_META[zone],
          },
        });
      });
    }

    // (No zone→host topology chain edges anymore. The ZoneBoundary
    // node now visually groups all hosts in a zone, so the thin
    // connecting lines from the old design are redundant. They were
    // also being read as "the firewall conduit went dotted" because
    // the chain ran straight down from the zone anchor through the
    // hosts at low opacity.)

    // Traffic view: render observed host-to-host flows when the toggle
    // is on. We aggregate by source-target pair so a single line
    // represents one logical conversation between two nodes - even if
    // the flow uses multiple protocols (RTAC↔relay carries both
    // Modbus and DNP3, for example). Aggregation is the difference
    // between 14 sprawling lines and ~9 readable ones.
    if (viewMode.traffic) {
      const presentNodeIds = new Set(styledNodes.map((n) => n.id));
      const nodeIndex = new Map(styledNodes.map((n) => [n.id, n]));

      // Group flows by `${source}→${target}` so each pair gets one
      // edge with a combined protocol label.
      const groups = new Map<string, ObservedFlow[]>();
      for (const flow of OBSERVED_FLOWS) {
        if (!presentNodeIds.has(flow.source) || !presentNodeIds.has(flow.target)) continue;
        const key = `${flow.source}->${flow.target}`;
        if (!groups.has(key)) groups.set(key, []);
        groups.get(key)!.push(flow);
      }

      const fZone = trafficFilter?.zone ?? "all";
      const fCrossOnly = trafficFilter?.crossZoneOnly ?? false;
      const anyHighlight = !!highlightedTrafficPair;

      Array.from(groups.entries()).forEach(([key, flows]) => {
        const first = flows[0];
        const sZone = nodeZone(first.source);
        const tZone = nodeZone(first.target);
        const crossZone = isCrossZone(first.source, first.target);

        // Apply the same filter the drawer uses so the canvas and
        // the matrix show the same subset. Filtered-out edges are
        // simply not emitted (no dim, just gone).
        if (fCrossOnly && !crossZone) return;
        if (fZone !== "all" && sZone !== fZone && tZone !== fZone) return;
        // A pair is "active" if any of its constituent flows is active.
        const statuses = flows.map((f) =>
          resolveLiveness(f, deviceComms, rtacOnline ?? false, trafficStatus),
        );
        const status: "active" | "idle" | "down" = statuses.includes("active")
          ? "active"
          : statuses.every((s) => s === "down")
          ? "down"
          : "idle";

        // Combined protocol label: dedup, preserve order, max 3.
        const protocols: string[] = [];
        for (const f of flows) {
          if (!protocols.includes(f.protocol)) protocols.push(f.protocol);
        }
        const label =
          protocols.length <= 3
            ? protocols.join(" + ")
            : `${protocols.slice(0, 2).join(" + ")} +${protocols.length - 2}`;

        // In the 62443 view, bands are stacked vertically, so traffic
        // flows top→bottom. Use default handles (no named handle) so
        // React Flow routes the bezier vertically from the source
        // node's bottom to the target node's top. Each node's unique
        // horizontal position within its band naturally spreads the
        // bezier midpoints horizontally - avoiding the overlap that
        // occurs when all edges share side handles.
        const sourceHandle = undefined;
        const targetHandle = undefined;

        const tooltipLines = [
          `${first.source} → ${first.target}`,
          ...flows.map((f) => `${f.protocol} on tcp/${f.port} · ${f.cadence}`),
          `category: ${first.category}`,
        ];

        const isHighlighted = highlightedTrafficPair === key;
        const isDimmedByHighlight = anyHighlight && !isHighlighted;
        const blockReason = viewMode.policyDim
          ? getFlowBlockReason(first.source, first.target, ruleSummaries)
          : null;
        const blockedByFw = blockReason !== null;

        styledEdges.push({
          id: `traffic-${key}`,
          source: first.source,
          target: first.target,
          sourceHandle,
          targetHandle,
          type: "trafficEdge",
          animated: status === "active" && !isDimmedByHighlight && !blockedByFw,
          data: {
            label,
            status,
            category: first.category,
            highlighted: isHighlighted,
            dimmedByHighlight: isDimmedByHighlight,
            blockedByFirewall: blockedByFw,
            blockReason: blockReason,
            tooltipLines,
          },
        });
      });
    }

    return { nodes: styledNodes, edges: styledEdges };
}
