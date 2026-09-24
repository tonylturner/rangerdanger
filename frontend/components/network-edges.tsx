"use client";

import React, { useEffect, useState } from "react";
import { createPortal } from "react-dom";
import { EdgeLabelRenderer, EdgeProps, getBezierPath } from "reactflow";

// Portal-based tooltip rendered in document.body. Tooltips inside SVG
// foreignObjects inherit opacity, transform, and clipping from their
// SVG parents, which made our edge tooltips look washed-out. Rendering
// in a portal sidesteps every one of those quirks: the tooltip is a
// plain absolutely-positioned div outside the React Flow tree.
function MapTooltip({
  visible,
  x,
  y,
  children,
}: {
  visible: boolean;
  x: number;
  y: number;
  children: React.ReactNode;
}) {
  const [mounted, setMounted] = useState(false);
  useEffect(() => setMounted(true), []);
  if (!mounted || !visible) return null;
  return createPortal(
    <div
      className="pointer-events-none fixed z-[60] rounded-lg border border-slate-700 px-3 py-2 text-[11px] text-slate-100 shadow-2xl"
      style={{
        left: x + 14,
        top: y + 14,
        backgroundColor: "#020617",
        maxWidth: 320,
      }}
    >
      {children}
    </div>,
    document.body,
  );
}

// Custom edge component with tooltip for firewall policy details. The
// label uses a pill background so it stays readable on the dark canvas
// and an action-aware leading icon (✓ allow / ✕ deny / ⚠ mixed) so the
// student can scan policy state without reading the label.
function PolicyEdge({
  id,
  sourceX,
  sourceY,
  targetX,
  targetY,
  sourcePosition,
  targetPosition,
  data,
  style,
  markerEnd,
}: EdgeProps) {
  const [tip, setTip] = useState({ visible: false, x: 0, y: 0 });

  const [edgePath] = getBezierPath({
    sourceX,
    sourceY,
    sourcePosition,
    targetX,
    targetY,
    targetPosition,
  });

  // Position the label ~60% along the path. With the firewall at
  // y≈50 and the zone-anchor target sitting on the boundary's top
  // edge, this puts the policy pill just above where the conduit
  // meets the zone - close enough to associate, far enough to clear
  // the boundary header label.
  const labelX = sourceX + (targetX - sourceX) * 0.6;
  const labelY = sourceY + (targetY - sourceY) * 0.6;

  const label = data?.label || "";
  const color = data?.color || "#64748b";
  const action = data?.action || "ALLOW";
  const details = data?.details || [];
  const dimmed = data?.dimmed === true;

  const actionIcon = action === "DENY" ? "\u2715" : action === "MIXED" ? "\u26A0" : "\u2713";
  const iconColor =
    action === "DENY" ? "#ef4444" : action === "MIXED" ? "#f59e0b" : "#22c55e";

  const labelWidth = Math.max(78, label.length * 6 + 28);
  const dimOpacity = dimmed ? 0.45 : 1;
  const pulse = data?.pulse === true;

  const zoneMeta = data?.zoneMeta as
    | {
        label: string;
        iface: string;
        role: string;
        subnet: string;
        fwIp: string;
        description: string;
      }
    | undefined;
  const hasTooltip = (details && details.length > 0) || !!zoneMeta;

  // Hover handler used by both the edge label and the wide invisible
  // hit-path so a student can hover anywhere on the line and get the
  // tooltip - not just the label pill.
  const showTip = (e: React.MouseEvent) => {
    if (hasTooltip) setTip({ visible: true, x: e.clientX, y: e.clientY });
  };
  const moveTip = (e: React.MouseEvent) => {
    if (tip.visible) setTip({ visible: true, x: e.clientX, y: e.clientY });
  };
  const hideTip = () => setTip((t) => ({ ...t, visible: false }));

  return (
    <>
      {/* Edge path + label group: dim with the policy view */}
      <g style={{ opacity: dimOpacity }}>
        <path
          id={id}
          style={style}
          className={`react-flow__edge-path ${pulse ? "tron-pulse" : ""}`}
          d={edgePath}
        />
        {/* Wide transparent hit-path so the tooltip triggers anywhere
            along the line, not only over the small label pill. */}
        <path
          d={edgePath}
          fill="none"
          stroke="transparent"
          strokeWidth={20}
          onMouseEnter={showTip}
          onMouseMove={moveTip}
          onMouseLeave={hideTip}
          style={{ cursor: hasTooltip ? "help" : "default" }}
        />
        {label && (
          <g
            onMouseEnter={showTip}
            onMouseMove={moveTip}
            onMouseLeave={hideTip}
            style={{ cursor: hasTooltip ? "help" : "default" }}
          >
            <rect
              x={labelX - labelWidth / 2}
              y={labelY - 11}
              width={labelWidth}
              height={22}
              rx={11}
              fill="#0f172a"
              stroke={color}
              strokeWidth={1.2}
              strokeOpacity={0.7}
            />
            <text
              x={labelX}
              y={labelY + 4}
              textAnchor="middle"
              fontSize={10}
              fontWeight={600}
              fill={color}
            >
              <tspan fill={iconColor}>{actionIcon}{"  "}</tspan>
              {label}
            </text>
          </g>
        )}
      </g>
      <MapTooltip visible={tip.visible} x={tip.x} y={tip.y}>
        {zoneMeta && (
          <>
            <div className="mb-1 text-[10px] font-bold uppercase tracking-wider text-sky-300">
              {zoneMeta.label} Interface
            </div>
            <div className="mb-1.5 grid grid-cols-[auto_1fr] gap-x-2 gap-y-0.5 text-[10px]">
              <span className="text-slate-500">Iface</span>
              <span className="font-mono text-slate-200">
                {zoneMeta.iface} <span className="text-slate-500">({zoneMeta.role})</span>
              </span>
              <span className="text-slate-500">Subnet</span>
              <span className="font-mono text-slate-200">{zoneMeta.subnet}</span>
              <span className="text-slate-500">FW IP</span>
              <span className="font-mono text-slate-200">{zoneMeta.fwIp}</span>
            </div>
            <div className="mb-2 text-[10px] leading-snug text-slate-400">
              {zoneMeta.description}
            </div>
          </>
        )}
        {details.length > 0 && (
          <>
            <div className="mb-1 text-[10px] font-semibold uppercase tracking-wider text-slate-500">
              Active Rules ({details.length})
            </div>
            <ul className="space-y-1">
              {details.slice(0, 8).map((detail: string, i: number) => {
                const isAllow = detail.startsWith("ALLOW");
                const isDeny = detail.startsWith("DENY");
                return (
                  <li key={i} className="flex items-start gap-1.5 leading-tight">
                    <span
                      className={`shrink-0 text-[10px] font-bold ${
                        isDeny ? "text-red-400" : isAllow ? "text-green-400" : "text-slate-400"
                      }`}
                    >
                      {isDeny ? "\u2715" : isAllow ? "\u2713" : "\u2022"}
                    </span>
                    <span className="text-slate-200">
                      {detail.replace(/^(ALLOW|DENY)\s*/, "")}
                    </span>
                  </li>
                );
              })}
              {details.length > 8 && (
                <li className="pl-4 italic text-slate-500">
                  +{details.length - 8} more rules...
                </li>
              )}
            </ul>
          </>
        )}
      </MapTooltip>
    </>
  );
}

// Traffic edge - observed peer-to-peer flow between two host nodes.
// Bezier curve from the source handle to the target handle. The
// handles sit on the visible edge of the node so the path connects
// directly to the icon outline. A triangle at the target gives
// unambiguous direction; no source-side dot because it was drifting
// away from the node and reading as a loose artifact.
function TrafficEdge({
  id,
  sourceX,
  sourceY,
  targetX,
  targetY,
  sourcePosition,
  targetPosition,
  data,
  style,
}: EdgeProps) {
  const [tip, setTip] = useState({ visible: false, x: 0, y: 0 });
  const [blockedTip, setBlockedTip] = useState({ visible: false, x: 0, y: 0 });

  const [edgePath, centerX, centerY] = getBezierPath({
    sourceX,
    sourceY,
    sourcePosition,
    targetX,
    targetY,
    targetPosition,
    curvature: 0.4,
  });
  const labelX = centerX;
  const labelY = centerY;

  const label = data?.label || "";
  const status: "active" | "idle" | "down" = data?.status || "idle";
  const tooltipLines: string[] = data?.tooltipLines || [];

  const isActive = status === "active";
  const isDown = status === "down";
  const isScenario = data?.category === "scenario";
  const highlighted = data?.highlighted === true;
  const dimmedByHighlight = data?.dimmedByHighlight === true;
  const blockedByFw = data?.blockedByFirewall === true;

  // Scenario-generated traffic renders in amber so students can
  // instantly distinguish "what the lab injected" from "what the
  // autonomous baseline is doing" (which stays cyan).
  const baselineActive = "#22d3ee";  // cyan-400
  const scenarioActive = "#f59e0b";  // amber-500
  const activeColor = isScenario ? scenarioActive : baselineActive;

  // Line color stays normal regardless of blocked state - red lines
  // are reserved for future attack traffic. Blocked flows are
  // indicated only by a red ✕ at the target device.
  const stroke = highlighted
    ? "#ffffff"
    : isActive
    ? activeColor
    : isDown ? "#64748b" : "#64748b";
  const labelColor = highlighted
    ? "#ffffff"
    : isActive
    ? (isScenario ? "#fbbf24" : "#67e8f9")
    : "#94a3b8";
  const extraGlow = highlighted
    ? `drop-shadow(0 0 10px ${activeColor}) drop-shadow(0 0 20px ${activeColor}88)`
    : undefined;
  const overrideOpacity = dimmedByHighlight ? 0.2 : undefined;
  const labelWidth = Math.max(56, label.length * 6 + 16);

  const dx = targetX - sourceX;
  const dy = targetY - sourceY;
  const edgeLen = Math.max(1, Math.sqrt(dx * dx + dy * dy));
  const ux = dx / edgeLen;
  const uy = dy / edgeLen;

  // Triangle sits close to the node (6px inset). The red ✕ sits
  // further back (22px) so there's hover room for its tooltip.
  const triX = targetX - ux * 6;
  const triY = targetY - uy * 6;
  const xMarkX = targetX - ux * 22;
  const xMarkY = targetY - uy * 22;

  // Labels sit at the bezier midpoint - no perpendicular offset.
  // The natural curve of the bezier + the vertical stagger of
  // nodes within each zone column creates a clean vertical column
  // of labels between zones.

  const arrowSize = 8;
  const arrowAngle = Math.atan2(dy, dx);
  const a1x = triX - arrowSize * Math.cos(arrowAngle - Math.PI / 6);
  const a1y = triY - arrowSize * Math.sin(arrowAngle - Math.PI / 6);
  const a2x = triX - arrowSize * Math.cos(arrowAngle + Math.PI / 6);
  const a2y = triY - arrowSize * Math.sin(arrowAngle + Math.PI / 6);

  return (
    <>
      <g
        onMouseEnter={(e) =>
          tooltipLines.length > 0 && setTip({ visible: true, x: e.clientX, y: e.clientY })
        }
        onMouseMove={(e) =>
          tip.visible && setTip({ visible: true, x: e.clientX, y: e.clientY })
        }
        onMouseLeave={() => setTip((t) => ({ ...t, visible: false }))}
        style={{ cursor: "help" }}
      >
        <path
          id={id}
          d={edgePath}
          className="react-flow__edge-path"
          fill="none"
          style={{
            stroke,
            strokeWidth: highlighted ? 3 : isActive ? 2 : 1.3,
            strokeDasharray: isActive ? "6 4" : "3 4",
            opacity: overrideOpacity ?? (isDown ? 0.45 : isActive ? 0.95 : 0.65),
            filter: extraGlow,
            ...style,
          }}
        />
        {/* Direction triangle - SVG, stays in edge layer */}
        {!blockedByFw && (
          <polygon
            points={`${triX},${triY} ${a1x},${a1y} ${a2x},${a2y}`}
            fill={stroke}
            opacity={isDown ? 0.5 : isActive ? 1 : 0.8}
          />
        )}
      </g>

      {/* Protocol label + red X rendered via EdgeLabelRenderer so
          they sit in the HTML layer ABOVE nodes. SVG edge elements
          render below nodes and can't receive hover when a node
          div overlaps them. */}
      <EdgeLabelRenderer>
        {/* Protocol label pill */}
        <div
          className="nodrag nopan pointer-events-auto"
          style={{
            position: 'absolute',
            transform: `translate(-50%, -50%) translate(${labelX}px, ${labelY}px)`,
          }}
          onMouseEnter={(e) =>
            tooltipLines.length > 0 &&
            setTip({ visible: true, x: e.clientX, y: e.clientY })
          }
          onMouseMove={(e) =>
            tip.visible &&
            setTip({ visible: true, x: e.clientX, y: e.clientY })
          }
          onMouseLeave={() => setTip((t) => ({ ...t, visible: false }))}
        >
          <div
            className="cursor-help whitespace-nowrap rounded-full border px-2 py-0.5 text-center text-[9px] font-semibold"
            style={{
              backgroundColor: '#0f172a',
              borderColor: `${stroke}cc`,
              color: labelColor,
            }}
          >
            {label}
          </div>
        </div>

        {/* Red X blocked marker */}
        {blockedByFw && (
          <div
            className="nodrag nopan pointer-events-auto"
            style={{
              position: 'absolute',
              transform: `translate(-50%, -50%) translate(${xMarkX}px, ${xMarkY}px)`,
            }}
            onMouseEnter={(e) =>
              setBlockedTip({ visible: true, x: e.clientX, y: e.clientY })
            }
            onMouseMove={(e) =>
              blockedTip.visible &&
              setBlockedTip({ visible: true, x: e.clientX, y: e.clientY })
            }
            onMouseLeave={() =>
              setBlockedTip((t) => ({ ...t, visible: false }))
            }
          >
            <div
              className="flex h-7 w-7 cursor-help items-center justify-center rounded-full"
              style={{
                backgroundColor: 'rgba(239, 68, 68, 0.2)',
                boxShadow: '0 0 8px rgba(239, 68, 68, 0.3)',
              }}
            >
              <svg width="14" height="14" viewBox="0 0 14 14">
                <line x1="2" y1="2" x2="12" y2="12" stroke="#ef4444" strokeWidth="2.5" strokeLinecap="round" />
                <line x1="12" y1="2" x2="2" y2="12" stroke="#ef4444" strokeWidth="2.5" strokeLinecap="round" />
              </svg>
            </div>
          </div>
        )}
      </EdgeLabelRenderer>

      {/* Tooltips via portal */}
      <MapTooltip visible={tip.visible} x={tip.x} y={tip.y}>
        <div className="mb-1 flex items-center gap-1.5 text-[9px] font-semibold uppercase tracking-wider text-cyan-400">
          <span className="h-1.5 w-1.5 rounded-full" style={{ backgroundColor: stroke }} />
          Observed flow · {status}
        </div>
        {tooltipLines.map((l, i) => (
          <div key={i} className="text-slate-200">{l}</div>
        ))}
      </MapTooltip>
      <MapTooltip visible={blockedTip.visible} x={blockedTip.x} y={blockedTip.y}>
        <div className="mb-1 flex items-center gap-1.5 text-[9px] font-semibold uppercase tracking-wider text-red-400">
          <span className="h-1.5 w-1.5 rounded-full bg-red-500" />
          Blocked by firewall
        </div>
        <div className="text-[10px] text-slate-200">
          {data?.tooltipLines?.[0] || ''}
        </div>
        {data?.blockReason && (
          <div className="mt-1 rounded border border-red-800/40 bg-red-950/20 px-2 py-1 text-[10px] leading-snug text-red-300">
            {data.blockReason}
          </div>
        )}
      </MapTooltip>
    </>
  );
}

// Edge types registration
export const edgeTypes = {
  policyEdge: PolicyEdge,
  trafficEdge: TrafficEdge,
};
