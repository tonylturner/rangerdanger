export function InfrastructurePlanes() {
  return <details className="shrink-0 text-xs text-slate-300">
    <summary className="cursor-pointer rounded focus-visible:outline focus-visible:outline-2 focus-visible:outline-sky-400">Infrastructure planes</summary>
    <dl className="mt-2 max-w-3xl space-y-2">
      <div><dt className="font-semibold">Teaching conduits</dt><dd>The map draws teaching-zone conduits through containd, not every network path. Declared topology, observed liveness and authorization are separate evidence.</dd></div>
      <div><dt className="font-semibold">Management / orchestration</dt><dd>Portal, backend, containd and designated simulator APIs use a separate management plane. Portal OpenPLC access is management access, not an engineering conduit.</dd></div>
      <div><dt className="font-semibold">Process truth</dt><dd>The internal process plane connects the solver and device simulations without containd; it is intentionally outside the segmentation map and is not a teaching conduit.</dd></div>
      <div><dt className="font-semibold">Desktop delivery</dt><dd>Isolated proxy-to-desktop delivery networks provide browser access, not teaching conduits. Desktop forwarding is disabled; delivery is not a route between teaching zones.</dd></div>
      <div><dt className="font-semibold">Same-zone and time boundaries</dt><dd>RTU→PLC and RTU→clock stay inside the station zone and do not traverse containd. Station time is simulated over HTTP, not NTP/PTP. Port rules do not distinguish reads from writes; command-level filtering would need DPI.</dd></div>
    </dl>
  </details>;
}
