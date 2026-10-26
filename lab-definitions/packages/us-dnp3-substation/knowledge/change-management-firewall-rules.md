Lab 2.4's evidence package is not a lab artifact for its own sake. It is a deliberately compressed version of what a real utility *change board* expects to see before approving a segmentation change. This article explains what a change board actually is, what they want, and how the lab's outputs map to that.

### What a Utility Change Board Is

A change board (sometimes called CAB - Change Advisory Board) is the operational governance body that reviews and approves changes to production systems. In OT environments these reviews are more stringent than in IT because:

- **Outages cost money and reputation** (the customer-service tile in the lab's HMI is the operational version of this - minutes of outage, customers affected, regulatory implications).
- **Some changes can damage equipment** (a misconfigured firewall rule that prevents the RTAC from reaching the recloser will fail to recover from a fault, which can damage the recloser or the upstream feeder).
- **Some are regulated.** NERC CIP-005 R1 requires utilities to document and review electronic-security-perimeter changes; CIP-010 requires baseline configurations and change-management evidence. A change board's records are part of the audit trail the regulator will eventually inspect.

A typical change board for a substation firewall change includes: a control-room operator (verifies operations impact), a protection engineer (verifies the change does not break protection coordination), a cybersecurity engineer (verifies the security posture is improved or unchanged), and an IT/network engineer (verifies the firewall configuration itself). The board reviews the proposed change package, asks questions, and votes to approve, defer, or reject.

### What the Change Package Has to Contain

A complete substation-firewall change package usually includes:

1. **Statement of intent.** What problem is this change solving? What is the operational or security gap? Why now?
2. **Configuration diff.** The current policy, the proposed policy, and the line-by-line difference. For a containd policy, this is the JSON diff plus the human-readable summary of "rule X added, rule Y removed, rule Z modified."
3. **Test evidence.** Proof that the proposed policy was tested before being proposed for production. For each new or modified rule: a positive test (the legitimate traffic still works) and a negative test (the previously-allowed bad traffic is now blocked). The evidence is typically a combination of PCAP captures, firewall logs, and screenshots of the test result.
4. **Rollback plan.** If the change goes badly in production, how do you revert? For a containd configuration rollback: import the old config → review the candidate diff → commit → verify the running hash and operations. Import (`containd cli> import config` or `POST /api/v1/config/import`) only stages the shared candidate; it does not change running. Review `GET /api/v1/config/diff`, preserving the management reservation and interface device bindings, then run `commit` (or `POST /api/v1/config/commit`). Verify the running hash matches the saved prior version and check both required connectivity and device operations.
5. **Maintenance window.** When will the change be applied? What other activities are scheduled in the same window? What is the impact on operations during the change itself (does the firewall need to be restarted)?
6. **Post-change monitoring plan.** What will you watch for in the hours after the change to confirm operations are healthy? Which dashboards, which alarms, which log queries?

A configuration rollback is not a binary or stored-schema rollback. Importing
an old policy does not undo containd's one-way stored schema 0.1.7 migration.
A binary downgrade needs the complete matching pre-upgrade firewall `/data`
backup and older image/release, as described in the operator upgrade guide;
an older image alone cannot read migrated storage.

### How the Lab Maps to This

Lab 2.4's evidence-assembly step produces almost exactly this package:

- **Statement of intent** - Lab 1.3's design verdicts and Lab 1.4's plan together are the *intent* document. The student wrote them.
- **Configuration diff** - `containd cli> export config` reads running, not the candidate. Save that output as `student-policy.json` after committing and testing the policy. It is the committed, tested policy submitted to the board, not an uncommitted proposal. Compare it with the saved weak baseline using `diff substation-weak.json student-policy.json`; that baseline is the pre-change reference, not necessarily what is running now. Before commit, review the proposed policy through the candidate and `GET /api/v1/config/diff`, not export.
- **Test evidence** - Lab 2.4's positive-tests step is the legitimate-traffic-works half. The negative-tests step is the bad-traffic-blocked half. Correlate fresh timestamped log entries on the Live DPI Events strip with each probe; a retained old DENY is not evidence of this attempt. The L4 source pin enforces; DPI detects and logs.
- **PCAP** - Lab 2.4's PCAP-capture step writes `/data/captures/validation.pcap` showing only RTAC sources reaching field on Modbus/DNP3 after the policy is applied. This is direct network-level evidence.
- **Audit log** - `containd cli> show audit` snapshots the `config.commit` entries that prove the policy was actually applied.

A real change board would expect more (rollback plan, maintenance window, monitoring plan), but the lab covers the *technical* portion of the evidence package end to end. The operational portions (when, who, what else) are organizational practice no lab can simulate.

### Why This Matters Beyond the Lab

Most cyber-trained workshop attendees never see a change-board package and have never had to defend a firewall change against operators who would rather not change anything. The lab compresses this experience into a 15-minute exercise on purpose. Walking out of the workshop with one assembled evidence package in your Exercise Notes gives you a tangible template to point at when your real organization asks "what would a good change package look like?"

The `scripts/validation-report.sh` helper requests the backend's markdown report for the policy currently running in the ready range; it does not apply a policy. When an applied plan is active, the report includes its plan proof alongside Hardened acceptance as the comparison. The resulting evidence is operator-facing rather than student-facing and can be attached directly to a change request.
