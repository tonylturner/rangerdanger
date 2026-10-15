**Default-deny** is the bedrock rule of network segmentation: anything not explicitly allowed is dropped. It's how you make "least privilege" enforceable instead of aspirational. But the *mechanism* for default-deny varies a lot across firewalls - and getting it wrong on an OT network can either (a) leave you wide open, or (b) take down a substation. Worth understanding which model your firewall uses.

### Two Models

**Implicit default-deny.** The policy engine appends a "deny everything not matched" verdict at the bottom of every rule list automatically. You write only ALLOW rules; anything that falls off the end is dropped. This is how most modern NGFWs work.

**Explicit default-deny.** Nothing is appended for you. You must either (a) configure the chain/policy default to DENY/DROP, or (b) write a final deny rule yourself. If you forget, the default is to *allow* and your policy is the inverse of what you intended. This is how iptables/nftables, raw Linux kernel netfilter, some Linux bridge setups, and a couple of older perimeter firewalls work.

The failure mode for explicit default-deny is silent: you build out your allow rules, you do not write a final deny, traffic appears to be working - but it is working because *all* traffic is allowed, not because your rules are correct.

### How the Vendors Handle It

| Firewall | Default-deny model | Notes |
|---|---|---|
| **containd** (this lab) | Implicit (default = DENY) | Policy has a top-level `defaultAction: DENY` field, already set out of the box. Adding broad ALLOW rules over the top creates a "weak" posture; deleting them re-exposes the implicit DENY. |
| **Cisco ASA / FTD** | Implicit | Every ACL has an implicit `deny any any` at the end. Best practice still adds an explicit `deny ip any any log` for visibility. |
| **Palo Alto NGFW** | Implicit (configurable) | Has two predefined rules at the bottom: `intrazone-default` (allow) and `interzone-default` (deny). Cannot delete them; can clone and modify (e.g., to add logging). |
| **Fortinet FortiGate** | Implicit | Implicit deny at the end of every policy table per VDOM. `set match-vip enable` and a final explicit deny is a common audit-friendly pattern. |
| **Juniper SRX** | Implicit | Implicit deny at the end of every from-zone/to-zone policy list. |
| **Check Point** | Implicit BUT best practice is explicit | Has an implicit cleanup rule but it does not log. Almost every Check Point install adds an explicit final `cleanup rule` of `any any drop log` so denies are visible. |
| **pfSense / OPNsense** | Mixed | Default-deny on WAN, default-allow on LAN (via auto-generated rules). Easy to misconfigure on multi-interface designs. |
| **iptables / nftables (raw)** | Explicit | Chains default to ACCEPT. You must run `iptables -P FORWARD DROP` (set the chain policy) or end every chain with an explicit drop rule. Forget either, and the firewall is allow-all. |
| **Linux bridge / OVS** | Explicit | Same problem class as raw iptables. Bridges pass traffic unless explicitly filtered. |

The thing students consistently get wrong on engagements is conflating the *models*: assuming a Cisco-style implicit deny is also there on their iptables jump host, or assuming a Palo Alto-style "interzone-default" rule exists on a fresh Linux bridge. The rule of thumb: if you cannot point to *where* the default-deny is - either as an implicit policy engine behavior with vendor documentation, or as an explicit rule you wrote yourself - assume there is no default-deny.

### Why This Matters in OT

In enterprise IT, the worst case of an unintended default-allow is data exposure or a security finding. In OT, it can be a kinetic event. A firewall sitting in front of a substation that *should* be denying enterprise → field Modbus, but is silently allowing it because someone forgot the explicit drop rule, leaves the recloser and the substation breaker reachable from an attacker's laptop. That is the configuration that the canonical Lab 2.3 attack exploits.

The flip side is also painful: a misconfigured default-deny that drops the RTAC's poll traffic takes the SCADA view of the substation offline. Operators lose telemetry. If protective relays cannot reach the historian, alarm correlation breaks. So OT segmentation work is always: get the default-deny right *first*, then carefully open the narrow set of flows the substation actually needs, then verify both halves (allowed flows work, denied flows do not) before signing off.

### How to Audit It

Three checks that catch most default-deny misconfigurations:

1. **Where is the implicit deny documented?** If it is vendor-implicit, link to the vendor's docs in your change record. If it is explicit, point at the rule by ID or line number.
2. **What does a packet for an undocumented flow do?** Generate one and check the firewall log. If the log is silent, the firewall is probably allowing - implicit-deny vendors typically don't log implicit denies unless you turn it on (Palo Alto's interzone-default rule, for example, does not log by default).
3. **What happens when you remove the last allow rule?** On a properly configured default-deny firewall, removing an allow rule should break the corresponding flow. If everything still works, the firewall is either bypassing the policy entirely (NAT/bridge misconfig) or has a hidden allow-all rule somewhere.

### What containd Does

containd's policy schema has a top-level `defaultAction` field on the firewall object. In every shipped configuration - including the deliberately-weak `substation-weak.json` - this is set to `"DENY"`. The weak baseline is "permissive" only because it adds broad cross-zone ALLOW rules above the default. The hardened policy keeps the same `defaultAction: DENY` and trims the allow list to the six flows the substation requires. The actual transition from weak to hardened is *removing rules*, not *changing the default*. That is the model the labs are built on.