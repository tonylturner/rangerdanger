Start a long-lived Kali monitor under the weak policy. Apply the improved policy. Compare the existing connection with a new GI attempt that is denied. Connection tracking (conntrack) can preserve an established flow even when the policy denies new connections.

To isolate the Control Centre, copy the improved policy and remove its CC-to-RTU allow rule. Apply that policy and verify that it is active. Only then remove the exact CC-to-RTU conntrack tuple. Use its original direction:

```text
GET /api/firewall/sessions?dport=2404&src=10.60.60.20
POST /api/firewall/sessions/kill
{"proto":"tcp","src":"10.60.60.20","dst":"10.30.30.20","sport":<observed-port>,"dport":2404}
```

Require exactly one CC-to-RTU match and copy its observed source port. Do not guess a port, use a wildcard or send a TCP reset. Later traffic is checked against the restrictive policy. Detecting the link failure takes time. A deny event alone does not prove the operator link is down.

Compare the CC's link-down and stale-point journal entries with the device's local-protection audit and solver results. The operator retains old values while [local protection](#eu-feeder-breaker-protection) acts. Restore the improved policy. The CC reconnects and runs GI. Point quality recovers when fresh reports arrive, not when the policy changes. Check boot identity and journal sequence bounds. Missing history cannot prove that the exercise succeeded.
