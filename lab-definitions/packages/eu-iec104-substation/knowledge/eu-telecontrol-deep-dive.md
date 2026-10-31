This optional sidebar preserves the operational investigation outside the seven workshop break points. It is not a mandatory validator sequence. Export evidence before reset; do not substitute management truth for observed CC data.

### Trace the two hops
CC→RTU TCP/2404 carries Type 46 select and execute, ACT-CON and ACT-TERM. RTU→breaker TCP/502 carries the downstream FC6 request. Correlate selected/executed times, matching feedback and the device audit revision with its first_solve acknowledgement. CLI success does not independently prove matching physical feedback. SBO is sequencing, not source authentication; direct execute, expired select and mismatched execute are rejection cases, not new authorities.

### Old session versus new connection
A foreign monitor admitted under weak may survive a new deny because it is established. Keep its five-tuple and compare a NEW GI attempt after improved. A new timeout and continued old reception describe two different policy epochs. The [conntrack reference](#eu-established-sessions) documents exact CC tuple lookup/deletion; never flush unrelated sessions or invent a TCP reset. Removing only the CC→RTU allow in a temporary restrictive policy must precede session deletion, otherwise reconnect can succeed.

### Stale cache versus local protection
Record link_down → point_stale (IV+NT/link_lost) → local_protection_trip → first_solve → later link_up → gi_complete. A stale value is neither zero nor a current switch position. Autonomous S1 protection can trip and lock out CB1 while the CC retains its former closed indication. clear_fault removes the initiating fault but does not acknowledge lockout or restore supply. Reset is workshop restoration, not an operating switching order.

Recovery requires a later connection_seq and GI in the same cache epoch, with GOOD received values matching independent device state. A CC restart or journal overrun breaks the temporal proof; do not splice unrelated boots. Preserve UTC bounds, journal oldest/latest cursors, IOAs 1001/1002/2001/2002, device generation/revision, audit and policy identity. Export before restoration and verify closed, no trip/lockout and fresh observation afterward. See [reset acceptance](#eu-reset-acceptance).

### Reflection questions

These optional questions preserve the operational investigation; they are not additional graded workshop steps. A denied path can be established by strict active probes without mandatory logged-deny evidence.

#### baseline-reachable

Can an enterprise host request station data without a CC identity?

#### port-is-authority

Does permitting TCP/2404 distinguish an authorised open from an unauthorised close issued by that same allowed host?

#### existing-flow

Will a new deny necessarily sever an already established connection?

#### source-boundary

Is the new foreign attempt denied while the named CC still operates?

#### stale-is-zero

May an operator treat the stale current as a fresh zero measurement?

#### local-protection

Does severing telecontrol prevent this local protective trip?

#### fault-clear-restores

Is clearing the initiating fault sufficient to restore feeder service?

#### recovery-evidence

What demonstrates restored operator visibility rather than a socket?
