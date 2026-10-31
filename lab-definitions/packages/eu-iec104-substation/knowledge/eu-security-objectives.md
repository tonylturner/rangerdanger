“Block bad traffic” is not a complete security objective for a distribution substation. The network exists to support an electrical process, and the operator needs trustworthy observation even when that process changes unexpectedly. A useful objective names the protected function, the unwanted behavior, the evidence and the remaining limitation.

### Preserve the right functions

**Protection must remain local.** The feeder breaker trips on fault-current measurements without waiting for a CC command. Losing IEC 104 observation must not be interpreted as disabling protection. Test the trip through device audit and process evidence while separately showing what the operator could observe.

**Supervisory control must be deliberate.** The named CC should reach the RTU, execute the configured select-before-operate sequence and receive fresh feedback. Enterprise and vendor hosts should not become alternate masters. Merely having a TCP session does not establish authority.

**Voltage regulation must retain its dependencies.** OpenPLC needs valid OLTC measurements and a functioning request path. The improved design must preserve PLC 10.30.30.30 → OLTC 10.40.40.23:502 while denying unrelated sources. Otherwise a successful negative connectivity test could conceal a failed control loop.

**Evidence must retain uncertainty.** The CC keeps old values with stale/invalid quality after observation fails. The historian archives those observations rather than replacing them with field truth. An unknown voltage is not zero; a missing command result is not a failed operation.

### Turn objectives into acceptance statements

| Objective | Useful acceptance evidence | Insufficient shortcut |
|---|---|---|
| Exclude an unauthorized master | New Kali GI denied; legitimate CC still fresh | One scan screenshot |
| Preserve supervisory switching | Attempt-window command record and fresh matching feedback | Breaker already in desired position |
| Retain local protection | Local trip audit, solved fault outcome, CC stale interval | “Link disconnected” banner |
| Preserve regulation | Valid input, request, completed tap and bus response | PLC web page loads |
| Separate archive consumption | Enterprise reads historian; cannot ingest or initiate OT access | TCP/8080 is open |

Select a baseline before changing the policy. Record operating mode, fault section, tap, measurement validity and observation freshness. A test against an already stale device cannot demonstrate that a later change caused staleness.

### Balance integrity and availability

Confidentiality matters, but for these exercises the immediate consequences are usually loss of supply, unintended switching, voltage excursion and loss of trustworthy visibility. Restricting all traffic maximizes neither security nor availability: the operator may lose restoration capability while a legitimate automatic controller becomes blind.

Conversely, preserving every legacy path to avoid disruption leaves enterprise and maintenance hosts able to issue control traffic. The [communications matrix](#eu-zone-conduit-design) makes that trade-off explicit, rather than hiding it in an “allow OT” rule.

### State the residual risks

The allowed CC can still misuse valid commands. A permitted Modbus session is not limited to reads by the reference policy. Same-subnet RTU-to-PLC traffic is outside the routed firewall boundary. The portal's management APIs are lab infrastructure, not a production authorization system.

These limits belong next to the successes in the acceptance record. Do not translate a green validator into “secure substation” or a regulatory compliance claim. Write “the tested new enterprise connection was denied while the named operational conduits remained functional,” then retain the evidence needed to reproduce that conclusion.

For a defensible review, connect each objective to a [risk scenario](#eu-iec62443-risk-design), an owner and a [handover criterion](#eu-acceptance-handover). That is a stronger result than counting how many deny rules were added.
