import type { ReactNode } from "react";
import type { FieldState } from "../lib/operator-api";
import { formatUtcTime } from "../lib/operator-format";
import { DeviceDiagnostics, ProtectionDiagnostics } from "./physical-system-equipment";
import { Group, Row, num, yesNo } from "./physical-system-parts";

export function PhysicalDiagnostics({ field, audit }: { field: FieldState; audit: ReactNode }) {
  const { process: p, clock } = field;
  return <div className="min-w-0 space-y-5">
    <div className="grid min-w-0 gap-x-6 md:grid-cols-2">
      <Group title="Simulation engine">
        <Row label="Engine / profile">OpenDSS / {p.profile_id}</Row>
        <Row label="Solver epoch">{p.solver_epoch}</Row>
        <Row label="Solve seq · solved (UTC)">{p.solve_seq} · {formatUtcTime(p.solved_at)}</Row>
        <Row label="Device input stale" alert={p.truth_stale}>{yesNo(p.truth_stale)}</Row>
        <Row label="Fault section">{p.fault_section}</Row>
        <Row label="Load multiplier">{p.load_multiplier}</Row><Row label="Source / tap">{p.source_pu} p.u. / {p.tap ?? "unknown"}</Row>
        <Row label="Profile enabled">{yesNo(p.profile.enabled)}</Row>
        {Object.entries(p.profile.load_factors).map(([category, factor]) => <Row key={category} label={`${category} profile factor`}>{num(factor, 3, "×")}</Row>)}
        <Row label="Profile source">{num(p.profile.source_pu, 3, "p.u.")}</Row>
        {Object.entries(p.buses).map(([id, bus]) => <Row key={id} label={`${id} voltage / live`}>{num(bus.kv, 2, "kV")} / {yesNo(bus.live)}{p.truth_stale ? " · STALE" : ""}</Row>)}
      </Group>
      <Group title="Station time">
        {clock ? <>
          <Row label="Sync status" alert={clock.sync_status !== "locked"}>{clock.sync_status.toUpperCase()}</Row>
          <Row label="Time offset">{num(clock.time_offset_sec, 3, "s")}</Row>
          <Row label="Satellites">{clock.satellite_count}</Row>
          <Row label="Reported time (UTC)">{clock.reported_time}</Row>
          <Row label="Actual time (UTC)">{clock.actual_time}</Row>
          <Row label="Alarm / comms OK">{yesNo(clock.alarm)} / {yesNo(clock.comms_ok)}</Row>
          <Row label="Holdover started (UTC)">{clock.holdover_started || "—"}</Row>
          <Row label="Holdover drift">{num(clock.holdover_drift_ppm, 3, "ppm")}</Row>
          <Row label="NTP / PTP / IRIG-B">{yesNo(clock.ntp_enabled)} / {yesNo(clock.ptp_enabled)} / {yesNo(clock.irig_b_output)}</Row>
          <Row label="Last command source">{clock.last_command_source}</Row>
        </> : <p className="text-xs text-slate-400">No station clock declared.</p>}
      </Group>
    </div>
    <section aria-label="Device diagnostics"><h3 className="text-sm font-semibold text-slate-200">Device diagnostics</h3>
      <div className="grid min-w-0 gap-x-6 md:grid-cols-2">{Object.entries(field.devices).map(([id, device]) => <Group key={id} title={id}><DeviceDiagnostics device={device} /><ProtectionDiagnostics device={device} /></Group>)}</div>
      <Group title="Solver delivery">{Object.entries(p.devices).map(([id, d]) => <div key={id} className="mb-2">
        <Row label={`${id} delivery`} alert={!d.delivered}>{d.delivered ? "delivered" : "FAILED"} · solved rev {d.solved_revision ?? "unknown"} / current {d.revision ?? "unknown"}</Row>
        <Row label={`${id} delivered I / P / Q`}>{num(d.current_a, 1, "A")} / {num(d.p_mw, 2, "MW")} / {num(d.q_mvar, 2, "Mvar")}</Row>
      </div>)}</Group>
    </section>
    {audit}
  </div>;
}
