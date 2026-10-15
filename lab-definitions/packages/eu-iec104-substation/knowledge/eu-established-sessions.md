A stateful firewall remembers connections. Applying a rule that denies new TCP/2404 connections does not necessarily remove a connection already classified as established. This is why “the policy was applied” and “the operator lost observation” are separate claims requiring separate evidence.

### Compare old and new sessions

In the lab's weak-policy phase, start a long-lived Kali monitor using the [IEC 104 CLI](#tool-iec104cmd). Apply improved policy through the lab workflow. Compare the existing monitor with a newly started GI attempt from Kali. The old connection may continue while the new connection is denied.

That outcome is not proof that source restrictions are ineffective. It demonstrates a transition-state issue: the old connection was admitted under a different policy. Record when each session began, when the policy changed and which five-tuple each observation belongs to. Do not treat every packet to port 2404 as the same session.

### Isolate only the intended link

For the exercise that isolates the CC, copy improved policy and remove its `control-centre-to-rtu-iec104` allow rule. Apply and verify that restrictive policy before removing session state. The backend exposes these routes:

```text
GET /api/firewall/sessions?dport=2404&src=10.60.60.20&dst=10.30.30.20
POST /api/firewall/sessions/kill
{"proto":"tcp","src":"10.60.60.20","dst":"10.30.30.20","sport":<observed-port>,"dport":2404}
```

This is a route/body reference; `<observed-port>` must be replaced with the actual original-direction source port. Require exactly one matching TCP session. If there are zero or multiple candidates, stop and resolve the ambiguity. Never guess, use a wildcard, flush all sessions or replace this step with a forged TCP reset.

Deleting conntrack state does not itself send a reset. Subsequent traffic meets the active restrictive policy, and link-failure detection takes time. If the allow rule remains active, the connection can recover instead of demonstrating isolation.

### Build a temporal proof

Collect a before snapshot and the CC journal cursor, perform the prescribed intervention, then collect the after journal and state. The useful sequence is:

1. Fresh observations and a working operator link.
2. Restrictive policy confirmed active.
3. Exact session deletion acknowledged.
4. Reconnection attempts denied, link loss detected and points marked stale.
5. Local device/process events during that stale interval.
6. Improved policy restored, reconnect and GI, then fresh point recovery.

A deny event alone does not establish item 4. An open breaker at the end does not establish when or why it opened. A stale CC picture may retain the former closed position while [local protection](#eu-feeder-breaker-protection) has already opened the real device.

### Preserve the evidence boundaries

Check the CC boot/cache identity and journal oldest/latest sequence numbers. A restart or ring-buffer overrun can remove the very interval being tested. Report missing history as missing evidence, not as successful isolation.

Plan restoration before making the cut. Keep the known-good improved policy and a way to apply it through management. If legitimate traffic does not recover, investigate policy identity, the reconnect/GI sequence and point freshness separately. Repeatedly deleting sessions under an uncertain policy creates more disruption without answering which dependency failed.

Keep source-time invalidity distinct from value invalidity, and CC receipt times distinct from solver times. Recovery means fresh protocol observations arrived; a restored policy or green TCP connection is only a prerequisite. Use [curl exports](#eu-tool-curl-exports) for operator and field snapshots, keeping the two sources separate throughout the report.
