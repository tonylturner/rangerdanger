A handover should let the next operator explain what changed, what was proven and what remains uncertain without rerunning the whole exercise. A screenshot of a green validator is useful context, but it cannot replace a scoped evidence package and an explicit acceptance decision.

### Identify the tested configuration

Record the package/revision, active policy identity, communications matrix and configuration diff. State the exercise window, operating conditions and people responsible for application, observation and acceptance.

Include CC cache identity, journal bounds and device generations/revisions where relevant. If a service restarted or a reset occurred, mark that boundary in the timeline. Evidence from different epochs must not be silently presented as one continuous observation.

The [change-resource plan](#eu-change-resource-plan) should already define the expected outcome and rollback criteria. Handover records what actually happened against that plan.

### Assemble a paired test record

| Claim | Evidence to retain |
|---|---|
| Unauthorized master excluded | New Kali attempt, active improved policy and matching network evidence |
| Authorized observation preserved | Fresh CC points, working GI/link and healthy-peer freshness |
| Switching path works | Attempt-window select/execute result, fresh feedback and device consequence |
| Automatic regulation preserved | PLC heartbeat/input validity, request outcome, settled tap and bus response |
| Archive separation works | Enterprise read success, denied enterprise ingest and no historian OT initiation |
| Recovery works | Restored policy, reconnect/GI and fresh quality after the disturbance |

For each row, write the actual result and limitations rather than only “pass.” A negative test without a healthy positive path may simply show that the service was unavailable to everyone.

### Keep three kinds of truth separate

Operator state is what the CC learned through IEC 104. Field/process state explains device and electrical behavior. Historian history is a series of CC snapshots with ingestion timing, quality and gaps.

Retain raw JSON and PCAP alongside any summary table. Keep source time, CC receipt time and historian ingestion time distinct, especially during clock exercises. A final matching value does not prove that the relevant operation occurred within the attempt window.

Command outcomes must retain `unknown` when acknowledgements or feedback are missing. Do not retry blindly to turn an uncertain result into a convenient completion. Missing journal history or capture loss is an evidence limitation, not a successful test.

### Close the operating state

After the exercise, restore the agreed policy and operating configuration. Verify fault state, F2 latch, RMU positions, earth/local state, OLTC mode/tap, PLC validity and CC freshness. Reset Lab is a prescribed recovery operation, not proof that the preceding switching order was correct.

The historian is not cleared by Reset Lab. New CC cache epochs create separate archive segments, so preserve that distinction when reviewing a trend after reset. Its snapshots do not establish sequence-of-events completeness.

### Accept residual risk explicitly

The reference policy is source/port based. A compromised permitted CC or PLC remains a risk; same-subnet RTU/PLC traffic is not a routed firewall boundary. The management plane and single-host containers are range infrastructure, not a production security architecture.

Name the owner of unresolved issues, the monitoring plan and a review date. Regulatory relevance may be discussed through [NIS2/NCCS](#eu-dso-regulatory-context), [BDEW](#eu-bdew-security-context) and [IEC 62443 risk design](#eu-iec62443-risk-design), but no lab result establishes conformity.

The final verdict should be narrow enough to defend: “These new unauthorized flows were denied, these required operations completed with fresh evidence, and these residual risks were accepted or deferred by the named owner.”

### Completion is scoped evidence

Lab 2.2 requires a fresh post-commit CC session, current-connection GI, every inventory point non-null/GOOD since GI start, the operational conduit proofs, independent breaker/RMU baseline positions and fresh section supply, plus active negative probes. A bad segmentation change fails visibility even when physical supply survives.

Lab 2.3 requires that same fresh CC visibility, restored breaker/fresh supply, active negative probes and bounded absence of foreign breaker operations since hardening. PASS does not certify that the earlier attack/restoration was performed, future DPI or regulatory compliance. The bonus certifies its sampled negative paths and restored RMU/OLTC state, not attack completion, supply or fresh CC observations.

Lab 2.4 additionally requires a backend-retained PASS report for this range, full running firewall hash and latest commit window. Hash reads bracket the probe run. Recommit/reapply or range/backend restart requires a new report; export it for durable handover. Current CC integrity, all field revisions delivered and fresh supply remain separate completion checks.
