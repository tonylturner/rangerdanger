Deep packet inspection (DPI) interprets supported application-protocol content rather than only network addresses and ports. In EU Improved it observes selected IEC 104 requests using three separate, explicit monitor rules. The L4 source pin enforces; DPI detects and logs. A would-deny match is not a denied command, and monitoring never satisfies a BLOCK requirement.

### Applied rules, not an admission policy

The six operational L4 allows and four logged diagnostic denies stay unchanged. Improved sets `dpiEnabled: true`, keeps global `dpiMode: enforce` and `nfqueueGroup: 101`, and appends the following rules. Each sets `protocol: iec104`, **`mode: monitor`**, `direction: request` and `ca: [1]`. The common scope is lan4→lan1, CC **10.60.60.20/32** → RTU **10.30.30.20/32**, TCP/2404.

| Rule ID | Configured action | IEC 104 selectors |
|---|---|---|
| `observe-cc-breaker-command` | DENY | `typeIds: [46]`, `ioa: [{start: 3001, end: 3001}]` |
| `observe-cc-oltc-step` | ALLOW | `typeIds: [47]`, `ioa: [{start: 3301, end: 3301}]`, `cot: [6]`, `commandStates: [1,2]`, `commandQualifiers: [0]`, `negative: false`, `test: false` |
| `observe-cc-oltc-raise-only` | ALLOW | Same OLTC selectors, but `commandStates: [2]` |

Apply Your Plan adds these only when `iec104-dpi` is selected. Weak is unchanged. Monitor rules never admit, deny, hold or reset traffic and open no enforcing envelope. Never overlay a monitor predicate on the operational CC allow: monitor ALLOW cannot admit the conduit. Explicit rule-local mode overrides global enforce; omission is not equivalent to monitor. Neither selected monitoring nor its hypothetical DENY establishes command authorization on an allowed CC session. Modbus DPI remains planning-only: an allowed TCP/502 path is not read-only or register-limited.

### Forwarding is still an engine dependency

Monitor traffic traverses **NFQUEUE 101**, adds engine processing latency and has **no queue bypass**. If the engine is down, the CC conduit drops. This change is not availability-neutral. Verify engine/DPI/IEC decoder health and independently prove fresh GI, PLC regulation and historian continuity, with rollback ready. Monitoring mode is not an engine-down forwarding guarantee.

### Read shadow evidence in containd

After every successful apply, **reconnect CC first** (`POST /api/operator/reconnect`), wait for connect GI and then take the baseline. Inspection starts only at a fresh SYN; a pre-apply session is not an inspection baseline. Read the embedded containd **Firewall/ICS tables**, not the RangerDanger event strip, for applied shadow counters. The underlying containd endpoint is `GET /api/v1/firewall/shadow-counters`.

Retain `available`, `snapshotVersion`, `since`, `unit`, and each rule's `ruleId`, `shadowMatches`, `wouldAllow`, `wouldDeny`. One hit means **one matching rule × one decoded message**, not an object, successful operation or prediction of the whole policy. All three applied monitor rules must be listed. A candidate-only rule is not applied, not zero. **`available:false` is never zero.** Successful apply resets counters; failed apply does not; engine restart loses them. If snapshotVersion or since changes, discard the comparison and reconnect/GI before a new baseline. Missing rules or unavailable counters are evidence gaps.

A prescribed approved CC CB1 operation can increment breaker `wouldDeny` while CB1 still operates under the L4 allow. Correlate fresh command feedback and actual field result; restore normal breaker/supply state. Matching events are `ics.decision`, `mode: monitor`, `decision: would_drop` (or `would_allow`), configured `action`, `reason: rule` and `ruleId`. Retain the UTC window. Events have bounded retention, not all-hit durable journaling. A missing event cannot establish zero traffic; a monitor DENY is not a delivered-command denial.

### Compare lower and raise, not just open ports

In Lab 2.2's manual OLTC exercise, take a stable counter baseline, then request one lower and wait for completed feedback. `observe-cc-oltc-step` must gain **≥1** match; `observe-cc-oltc-raise-only` must gain **0**. One raise restoring the tap must add **≥1** to both. Then perform the two-lower/auto regulation exercise and retain PLC/field/historian proof. Keep the snapshot and since unchanged throughout each comparison.

Type 47 RCS **1 is lower, 2 is raise**; Type 45 mode states instead use **0 manual, 1 auto**. QU 0 is the accepted qualifier here. `negative: false` and `test: false` constrain request P/N and T; false is not omission. COT 6 is separate from those flags. A fully decoded successful [SBO action](#iec104-sbo-commands) normally adds **2 matches per matching rule** (select + execute, both COT 6), but **not guaranteed**: early refusal can end the sequence. Require ≥1, never exactly two. Select/execute matching does not establish prior-selection history, operator identity or appropriate operating intent. These predicates do not constrain analog setpoint values. Do not interpret missing evidence as a successful non-match.

### Optional instructor-led PCAP Analysis — not graded

The instructor demonstrates and does not grade anything in this optional sidebar. Begin with a clean candidate and a classic PCAP covering SYN through an already approved controlled command. In embedded containd **PCAP Analysis**:

1. Import with **Analyze**. Inspect the sealed dataset, capture hash, decoder/profile/snapshot fingerprints, record counts, gaps and expiry. Analyze alone does not generate rules.
2. Explicitly **Generate suggestions**. Inspect certified frozen evidence, limitations and withheld reasons. Supported state/QU command tuples can certify; **SBO sequences, setpoint values/QL and bitstrings remain withheld**. Monitoring ASDUs/startup do not become command ALLOWs. Dataset gaps are limitations, not a blanket bar: retained complete eligible parents may certify.
3. An authenticated administrator must **Review** the exact IEC revision and acknowledge captured wire/NAT scope. Review is approval, not selection, staging or application. Select the reviewed revisions, then **Stage selected** atomically as explicit **`mode: monitor` only**.
4. Inspect the candidate diff and verify running policy remains unchanged. Certification is bounded evaluator evidence, not permission, delivery proof or universal protocol proof. A staged receipt does not prove an active rule; global enforce cannot override an explicit monitor mode.
5. **Commit is a separate deliberate step, not performed in this sidebar. End by discarding the candidate**, checking no uncommitted diff remains and the running policy/hash is unchanged. Starting with a clean candidate avoids discarding someone else's work.

Keep dataset/hash/revision/proof and withheld reasons separate from the 37-row L4 report. Analysis, generation and staging alone do not change the running policy. Monitoring alone does not start learning or collect a dataset.

### Handover only the claim tested

Keep applied policy/plan identity, source/destination/service, fresh-session boundary, counter lifetimes, rule-local deltas/events and independent command/device outcomes. Preserve the NFQUEUE forwarding dependency and rollback owner. Neither TCP proof nor decoded capture certifies command prevention. See [acceptance and handover](#eu-acceptance-handover), [established sessions](#eu-established-sessions) and [GI/quality](#iec104-gi-cot-quality).

References: containd v0.1.36 `docs/mkdocs/ics-dpi.md` (modes, shadow counters, IEC selectors, PCAP Analysis); EU `firewall/improved.json`, `curriculum/plan.yml`, `protocols/points.json`.
