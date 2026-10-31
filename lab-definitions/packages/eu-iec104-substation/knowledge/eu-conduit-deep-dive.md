This optional design/evidence sidebar supplements Labs 1.2–2.4 without changing their break points. Inventory each zone, conduit owner, source, destination, purpose, acceptance test and rollback.

### Data, time and non-forwarding
Keep six required intents: CC→RTU 2404, RTU→field 502, PLC→OLTC 502, CC→historian ingest 8081, enterprise→historian read 8080 and named eng-ws→PLC web 8080. RTU↔PLC and RTU↔GPS are same-subnet endpoint dependencies, not firewall enforcement. Historian lan5 has no initiating OT egress; replies to admitted reads are not a new authority. Distinguish source time, CC receipt time and archive ingestion time.

### Three footholds and full isolation

**Full vendor-isolation check:** review [zone design](#eu-zone-conduit-design).
Probe from Kali, vendor-jump and eng-ws separately. Record teaching addresses, not a backend management source. Full vendor-isolation review includes mgmt_net, containd 10.99.99.2, and desktop bridge gateways 10.98.2.2 and 10.98.3.2, not only teaching-port denies. Neither desktop has a lan3 interface. A management shortcut invalidates the claimed boundary; no new route, port or gateway is authorized by this sidebar. Review non-forwarding and rule order as well as sampled tests.

### Positive evidence stronger than sockets
Require a retained matched GI start/completion on the current connection within ten minutes and after the attempt boundary, with matching cache epoch; all 31 CC points must be non-null, GOOD and received at or after its start. Quiet status needs no three-second receipt cutoff. If GI evidence is missing or outside the window, press **Interrogate station** in the **Control Centre** and validate again; collect PLC/history proof after the GI boundary, not before. Periodic GIs do not restart a valid attempt. Observe real PLC running/input-valid/heartbeat plus an accepted PLC-source adjacent request, completed motion and solved revision. Historian proof needs at least two advancing snapshot_seq/ingested_at samples and in-window CC receipt times; preserve uncertainty. A real RTU→GPS HTTP read proves the local clock operation; GI objects are untimed and may clear source_time. Capture timestamped spontaneous points separately when examining clock provenance.

### Named maintenance and reversible changes
The plaintext PLC workshop UI is named eng-ws only, not general vendor station authority. Specify owner, expiry, change window, revocation test and future authenticated gateway. Preserve the seed identity; do not edit PLC logic to fix a firewall test. Rehearse rollback using exported known-good policy and establish a new evidence window afterward. Record actual role effort against the shared 500-hour plan, not a separate retired 80-hour exercise.

### Negative evidence and acceptance
New probes must come from each real source. Strict active probing counts only connect timeout as denied; refused means reached, and container/tool errors are failed evidence. Logs can corroborate but are not mandatory for default deny. A logged SYN cannot identify denied payload. Do not require old logged-deny evidence as the only acceptance path.

For each row retain planned verdict, measured result, UTC window, policy identity, evidence path, gap, owner and next action. Pair available operations with blocked exposure, not a named policy alone. [IEC 62443 risk design](#eu-iec62443-risk-design) and [resource plan](#eu-change-resource-plan) explain the engineering signoff.

### Reflection questions

These optional questions preserve the operational investigation; they are not additional graded workshop steps. A denied path can be established by strict active probes without mandatory logged-deny evidence.

#### cc-rtu

CC→RTU TCP/2404: what is the smallest acceptable conduit for GI, indications and SBO? Which controls still belong at the endpoint?

#### rtu-field

RTU→breaker/RMU1/RMU2/OLTC TCP/502: allow only 10.30.30.20/32, or the entire station? Include reads AND legitimate command writes.

#### plc-loop

PLC→OLTC TCP/502: does the PLC need breaker or RMU access? RTU→PLC FC3 is same-subnet and needs endpoint controls, not a claim that containd enforces it.

#### foreign-control

May enterprise, vendor or eng-ws bypass the RTU to ANY field device, or open their own IEC 104 session? Who accepts the residual risk from misuse of an already trusted CC, RTU or PLC?

#### historian-feed

CC 10.60.60.20→historian 10.70.70.40:8081 is POST ingest only. Does any other source need to submit observations?

#### historian-read

Enterprise→historian:8080 is GET-only. Should anything leave lan5 toward station, CC or field? How will you prove no write-back?

#### station-time

RTU→GPS 10.30.30.50:8080 is HTTP time polling, not NTP in this package. Both are on lan1: name endpoint controls and the firewall blind spot rather than promising cross-zone enforcement.

#### engineering-maintenance

What maintenance scope, owner, expiry and revocation test will you approve? The plaintext workshop UI is not a production remote-access design; record that residual risk and a future authenticated gateway.
