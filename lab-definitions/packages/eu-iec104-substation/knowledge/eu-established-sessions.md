Start a long-lived Kali monitor under weak policy, apply improved, then compare the surviving established flow with a newly denied GI. Existing conntrack state and new connection policy are different observations.

To isolate the Control Centre, first apply AND verify improved-minus-CC-allow. Only then remove the exact original-direction CC-to-RTU conntrack tuple:

```text
GET /api/firewall/sessions?dport=2404&src=10.60.60.20
POST /api/firewall/sessions/kill
{"proto":"tcp","src":"10.60.60.20","dst":"10.30.30.20","sport":<observed-port>,"dport":2404}
```

Require exactly one CC-to-RTU match and copy its observed source port. Do not guess a port, use a wildcard or send a TCP reset. Later traffic meets the restrictive policy; detecting failure takes time. A deny event alone does not prove the operator link is down.

Compare CC link-down and stale-point journal entries with the device's local-protection audit and solver progression. The operator retains old values while [local protection](#eu-feeder-breaker-protection) acts. Restore improved policy, reconnect and GI; fresh reports recover quality, not the policy change alone. Evidence must be bounded by boot identity and journal sequence; missing history is not a pass.

Sources: EU scenario lines 99–177; `backend/internal/server/firewall_sessions.go` lines 15–18, 24–99.
