A firewall change is an operating change when it affects observation, switching, voltage regulation or restoration. The change plan should therefore describe more than a JSON edit. It must say who is available, what they will prove, how long they will wait and what makes them stop.

### Define the intended outcome

Use a narrow statement: “Remove enterprise and vendor master access while preserving the named CC, RTU, PLC and historian conduits.” Attach the current and proposed policies and explain each added, removed or narrowed flow. The [zone/conduit matrix](#eu-zone-conduit-design) should agree with the policy diff.

Avoid “make the substation compliant” or “block all attacks.” Those are neither demonstrated outcomes nor measurable acceptance criteria.

### Allocate people and equipment

Assign an operating lead to approve process conditions, a network/security engineer to apply and verify policy, an engineering/PLC owner to confirm regulation, and an evidence recorder to preserve the attempt window. One student may perform several roles in the range, but the responsibilities remain distinct.

Prepare access to the portal, Kali terminal, engineering terminal where needed, the proposed policy and its known-good rollback reference. Reserve storage for raw JSON, capture files, command output and a concise timeline. Agree on the time source and record any clock-offset exercise separately.

The shared range is a single operating environment. Coordinate disruptive steps; another person's reset, fault injection or session cut can invalidate the baseline. Do not restart services merely to make a difficult test pass.

### Establish a clean baseline

Record active package and policy, CC cache identity and journal cursor, device generations/revisions, fault section, F2 state, RMU positions, OLTC tap/mode and PLC freshness. Confirm nominal operation before testing a disturbance.

Capture both operator-observed and field/process state. They answer different questions and must not be merged into one supposedly authoritative screenshot.

### Sequence tests around dependencies

First verify the policy is actually active. Then test a new unauthorized connection, continued legitimate CC observation, and the PLC/OLTC dependency. Check enterprise historian reads independently of CC ingest. The archive's continued existence does not prove new samples are arriving.

If the exercise needs an operator outage, follow the [exact-session procedure](#eu-established-sessions). Policy application alone may preserve established connections. Allow enough time for protocol failure detection and point staleness; do not shorten the observation window to obtain a green result.

For switching, retain the command attempt and fresh completion feedback within the same window. For automatic regulation, allow the ten-second continuous out-of-band delay and two-second step motion, then observe the completed tap and bus response.

### Define rollback before starting

Rollback triggers include loss of a required authorized conduit, unexplained stale peer devices, incorrect active policy or evidence gaps that prevent a safe decision. Name the person who can call rollback.

Restoring a known-good policy is only the first recovery step. Verify reconnect, GI and fresh points; confirm PLC input validity and stable process state. A rollback to weak policy deliberately reintroduces exposure, so record it as a temporary recovery state rather than a secure final configuration.

Close with [acceptance and handover](#eu-acceptance-handover): policy identity, positive/negative results, residual risks, unresolved observations, monitoring owner and next review. A failed assumption should become a tracked issue, not a silent broad allow added at the end of the window.
