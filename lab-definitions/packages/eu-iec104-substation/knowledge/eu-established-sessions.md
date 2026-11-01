A commit re-evaluates established connections on their next packet against the current first-match policy. Traffic the policy no longer admits stops. Sessions may still show the conntrack entry until it times out, so an entry is not proof that traffic passes. Application failure detection and point staleness can take longer than packet denial; preserve both the commit boundary and operator evidence.

### Compare old and new sessions

In the lab's weak-policy phase, start a long-lived Kali monitor using the [IEC 104 CLI](#tool-iec104cmd). Apply improved policy through the lab workflow. Compare the existing monitor with a newly started GI attempt from Kali. Both the existing monitor's traffic and the new connection should be denied if the current policy no longer admits that source.

A retained Sessions row is connection-tracking state, not surviving permission under the old policy. Record when each session began, when the policy changed and which five-tuple each observation belongs to. Do not treat every packet to port 2404 as the same session.

### Isolate only the intended link

For the exercise that isolates the CC, copy improved policy and remove its `control-centre-to-rtu-iec104` allow rule. Apply and verify that restrictive policy, then observe loss without deleting session state. Session revocation is optional: clear a retained entry or force a fresh handshake so DPI inspects from SYN. It is not needed to enforce a committed DENY. For that diagnostic, the backend exposes these routes:

```text
GET /api/firewall/sessions?dport=2404&src=10.60.60.20&dst=10.30.30.20
POST /api/firewall/sessions/kill
{"proto":"tcp","src":"10.60.60.20","dst":"10.30.30.20","sport":<observed-port>,"dport":2404}
```

This is a route/body reference; `<observed-port>` must be replaced with the actual original-direction source port. Require exactly one matching TCP session. If there are zero or multiple candidates, stop and resolve the ambiguity. Never guess, use a wildcard, flush all sessions or replace the diagnostic with a forged TCP reset.

The session-kill API first installs a revocation, then deletes conntrack state. By default it blocks the exact tuple in both directions for 60 seconds. TCP resets are signalled on each endpoint's next matching packet, not injected at deletion time or proof of confirmed endpoint closure. A fresh source port is allowed by the revocation and meets the active policy, so the restrictive policy is needed to prevent reconnection.

Check the response's per-step fields: `conntrackDeleted` and `revocationInstalled` must both be true, with no `deleteError` or `revocationError`. RangerDanger returns `status: "deleted"` only for full success; partial failures return HTTP 502 with `status: "failed"` and the outcome fields, including any `blockedUntil`. Do not infer success from a deletion count alone.

### Build a temporal proof

Collect a before snapshot and the CC journal cursor, perform the prescribed intervention, then collect the after journal and state. The useful sequence is:

1. Fresh observations and a working operator link.
2. Restrictive policy confirmed active; established traffic stops on its next packet without session deletion.
3. Reconnection attempts denied, link loss detected and points marked stale.
4. Local device/process events during that stale interval.
5. Improved policy restored, reconnect and GI, then fresh point recovery.

A deny event alone does not establish item 3. An open breaker at the end does not establish when or why it opened. A stale CC picture may retain the former closed position while [local protection](#eu-feeder-breaker-protection) has already opened the real device.

### Preserve the evidence boundaries

Check the CC boot/cache identity and journal oldest/latest sequence numbers. A restart or ring-buffer overrun can remove the very interval being tested. Report missing history as missing evidence, not as successful isolation.

Plan restoration before applying the restrictive policy. Keep the known-good improved policy and a way to apply it through management. If legitimate traffic does not recover, investigate policy identity, the reconnect/GI sequence and point freshness separately. Repeatedly deleting sessions under an uncertain policy creates more disruption without answering which dependency failed.

Keep source-time invalidity distinct from value invalidity, and CC receipt times distinct from solver times. Recovery means fresh protocol observations arrived; a restored policy or green TCP connection is only a prerequisite. Use [curl exports](#eu-tool-curl-exports) for operator and field snapshots, keeping the two sources separate throughout the report.

### Post-commit proof in Labs 2.2 and 2.3

Use `curl -fsS -X POST http://localhost:8088/api/operator/reconnect` from the host, or `wget -qO- --post-data '' http://10.99.99.21:8080/api/reconnect` in cc-1. This closes the actual IEC 104 connection via the CC link worker without the session-kill API's temporary tuple revocation. No policy, device or cache reset occurs. Wait for a strictly post-commit `link.since`, a matched completed GI on that connection, and every inventory point non-null/GOOD received since GI start. Delivery on an existing connection tests the current policy, but this separate fresh-link requirement checks handshake admission and inspection from SYN, not just continued application delivery.

CR-22 in Lab 2.2 omits the CC allow. Correct it before commit, or test only with rollback ready. The reference's explicit logged `deny-unauthorized-iec104` covers the CC zone after the allow, so new rejected CC connections appear in Live DPI Events. Correlate CC link loss, Network Map conduit and the deny tuple without calling observation loss a physical outage. A correct solution passes without ever losing telemetry. Silent blackhole detection uses t3+t1, plus reconnect and observation margin; G7 derives its deadline from `protocols/points.json` rather than assuming 30 s.
