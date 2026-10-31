import type { ZoneRuleSummary } from "../lib/api";
import { getFlowAuthorization } from "../lib/network-console-data";
import { CONNECTION_DIRECTION, flowPurpose, PORT_RULE_BOUNDARY } from "../lib/network-flow-meaning";
import type { FlowContext, FlowPair } from "../lib/observed-flows";

export function FlowEvidence({ context, pair, ruleSummaries }: {
  context: FlowContext; pair: FlowPair; ruleSummaries?: ZoneRuleSummary[];
}) {
  const authorization = getFlowAuthorization(pair.source, pair.target, ruleSummaries, context.graph);
  return <section aria-label={`Flow evidence ${pair.source} to ${pair.target}`} className="mt-2 space-y-2 border-t border-slate-700 pt-2 text-xs text-slate-300">
    <p><strong>Purpose:</strong> {pair.protocols.map(flowPurpose).join(" ")}</p>
    <dl className="space-y-2">
      <div><dt className="font-semibold">Topology</dt><dd>Declared by package · {pair.crossZone ? "teaching-zone conduit through containd" : "same zone · does not traverse containd"}.</dd></div>
      <div><dt className="font-semibold">Observed liveness: {pair.status}</dt><dd>{pair.protocols.map((flow) => {
        const observation = context.observations[flow.id];
        return <p key={flow.id}>{flow.protocol} :{flow.port} · {observation ? `${observation.evidence}: ${observation.reason}` : "Not observed"}</p>;
      })}Liveness is not authorization or proof of command success.</dd></div>
      <div><dt className="font-semibold">Authorization</dt><dd>{authorization.text}</dd></div>
    </dl>
    <p>{CONNECTION_DIRECTION}</p>
    <p>{PORT_RULE_BOUNDARY}</p>
  </section>;
}
