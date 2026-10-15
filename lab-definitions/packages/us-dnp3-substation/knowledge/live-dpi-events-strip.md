The Segmentation drawer on the [Network Map](/console) has a **Live DPI Events** strip that surfaces firewall events in real time - every accept, every deny, every DPI verdict. This article is the operator's manual for reading it.

### Where the Events Come From

containd emits two kinds of events that feed the strip:

1. **L4 events** - every packet that matches a rule with logging enabled. These come from the kernel's **nflog** facility on the `nflogGroup` configured in the active policy (group 100 in the lab's hardened config). A userspace consumer inside containd reads them and forwards them via Server-Sent Events to the backend, which republishes them on `/api/substation/network-events`.
2. **DPI events** - when a rule has `dpiMode: enforce` set, the packet is also queued to userspace via **NFQUEUE** (group 101 in the lab). The DPI engine parses the payload, applies the predicate, and emits a structured event with the verdict and the protocol-specific reason.

### Reading a Row

Each row in the strip looks roughly like:

```
12:34:56.789  category: l4    verdict: DENY   src: 10.10.10.50  dst: 10.40.40.20  port: 502   rule: enterprise-deny
```

The fields you actually care about for lab evidence:

- **timestamp** - when the packet arrived. Useful for correlating with your probe commands.
- **category** - either `l4` (decided by the kernel-side rule) or `ics` (decided by the userspace DPI verdict). The distinction is the lab's whole "L4 + DPI defense in depth" lesson.
- **verdict** - `ACCEPT` or `DENY`. (Plus `BlockFlowTemp` for the case where DPI temporarily blocks a flow to throttle abuse.)
- **src / dst / port** - the L4 5-tuple.
- **rule** - the rule ID that matched. Cross-reference against the active policy's JSON to see what the rule actually allows.
- **attributes** - for DPI events, the protocol-specific reason: `functionCode: 5` for a Modbus FC5 reject, `crobOpCode: trip` for a DNP3 trip reject, etc.

### Common Patterns in the Lab

**An L4 deny on a hardened policy.** You ran an enterprise → field probe. The kernel dropped the packet before DPI was reached. You see:

```
category: l4   verdict: DENY   src: 10.10.10.50   dst: 10.40.40.20   port: 502   rule: enterprise-deny
```

That is the L4 source-pin in action.

**An ICS DPI deny on a partially-allowed flow.** You ran a probe from a source the L4 rule allows but the DPI rule does not (e.g., an eng-ws-to-RTAC Modbus write). You see:

```
category: l4   verdict: ACCEPT   src: 10.20.20.20   dst: 10.30.30.20   port: 502   rule: eng-ws-to-rtac
category: ics  verdict: DENY    src: 10.20.20.20   dst: 10.30.30.20   port: 502   rule: rtac-dpi  attributes: { functionCode: 5 }
```

Both rows appear because the packet passed L4 and then got rejected by DPI. This is the *exact* condition the lab's "DPI matters even when L4 is loose" hint block in Lab 2.3 is trying to make visible.

**Lab 2.4 evidence assembly.** When you assemble the evidence package, run your negative-test probes one at a time and screenshot or copy out the matching event rows. The change board sees both the policy intent (your `student-policy.json` export) and the policy reality (these event rows). Two-source attestation.

### When the Strip Stays Empty

If you fire a probe and no row appears, the usual causes are:

- The probe never actually hit containd (kernel did not see it - check your source). If you probed from the wrong container, the packet may have stayed intra-zone.
- The matching rule has logging disabled. Lab policies enable logging on the deny-class rules; if you have applied your own custom policy without logging, your rules may match without emitting events.
- The `nflogGroup` is not set on the active profile. The lab's improved policy sets `dataplane.nflogGroup: 100`; if you exported and re-imported a policy that lost that key, the L4 events never reach containd's consumer.
- The frontend strip is filtered. The drawer has a category-and-verdict filter at the top; make sure it is not filtering out the rows you want to see.

The strip is the closest thing the lab has to a real-time firewall watch. Use it.