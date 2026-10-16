RMU operations are an optional operational deep dive, not extra required labs. Predict affected sections before touching a switch. The radial baseline is CB1 closed, RMU1 closed, RMU2 open; the normally-open tie avoids paralleling live sources. Retain physical supply and CC quality separately.

### Positions and authority
Compare the effects of RMU1 and RMU2 switching using observed positions 1101/1201 and both-side voltages. Positive ACT-CON alone is insufficient: match fresh feedback, ACT-TERM and RTU-source device writes. Local authority should refuse a remote operation with not_remote. Returning remote does not bypass earthing or live_live interlocks: closing a live-live tie must refuse without motion. Earthing, fault passage and remote/local state are distinct indications, not interchangeable switch positions.

### Fault, isolation and back-feed
An S1 fault causes a local protective trip. Record fault passage indicators before reset. Fault removal is not restoration. The safe reasoning order is trip → identify → isolation of S1 → confirmed open RMU1 → permitted RMU2 tie closure → back-feed of S2. Require the isolation completion before selecting the tie. Do not parallel live sources or label a reset a safe switching order. Record S3 voltage, and do not infer an unobserved F2 position from a symbol.

### Source attribution and handover
A vendor Modbus write under weak and an RTU-mediated CC command can have the same physical result but different provenance. Preserve accepted source peer, operation, device generation/revision and UTC window. Later same-source denial establishes a boundary; the denied SYN does not reveal command payload. Keep restored radial state and fresh observations for handover, plus ownership of each uncertainty.

See [switchgear](#eu-rmu-switchgear), [switching orders](#eu-rmu-switching-orders), [local protection](#eu-feeder-breaker-protection) and [acceptance](#eu-acceptance-handover). The seven-slot bonus uses one prescribed vendor wire operation rather than making this entire switching investigation mandatory.

### Reflection questions

These optional questions preserve the operational investigation; they are not additional graded workshop steps. A denied path can be established by strict active probes without mandatory logged-deny evidence.

#### rmu-predicted-loss

With RMU2 open, which loads lose supply when RMU1 opens?

Expected reasoning: s2 only.

#### rmu-distinct-effects

Why does opening the tie after transfer differ from opening RMU1?

Expected reasoning: different paths.

#### rmu-local-authority

Which boundary refused a valid remote command delivered over an allowed conduit?

Expected reasoning: device authority.

#### rmu-parallel

What is the expected refusal with fresh measurements and both tie sides live?

Expected reasoning: live live.

#### rmu-fpi-meaning

What does a clear RMU1 FPI mean for the specified S1 fault?

Expected reasoning: no passage at rmu1.

#### rmu-restoration

Which sequence restores S2 without reconnecting the S1 fault?

Expected reasoning: isolate then backfeed.

#### rmu-vendor-authorised

What does the successful weak-policy write demonstrate?

Expected reasoning: reachable not authorised.

#### rmu-improved-evidence

Which evidence distinguishes a working boundary from a broken device?

Expected reasoning: deny plus allowed operation.
