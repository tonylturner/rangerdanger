The implemented breaker command is IOA 3001: DCS 1 opens and DCS 2 closes. Select-before-operate (SBO) creates a selection that expires after 5000 ms. Execute must match the selected peer, command, value and qualifier. Direct, expired and mismatched executes reject.

The RTU sends Modbus FC6 once. A positive echo leads to ACT-CON, COT 11 feedback and ACT-TERM. The CC additionally requires matching feedback after execute confirmation before declaring completion.

A `rejected` command received a negative select or execute confirmation. `failed` means execute was never sent, for example because the link was lost first or select confirmation was missing. `unknown` means execute was sent but its outcome is ambiguous: confirmation, termination or matching feedback may be missing. Do not retry an ambiguous command automatically: another execute is another operation. The CLI's result alone is not the CC's physical-completion proof.

SBO is neither identity authorization nor firewall deep packet inspection. An allowed CC can open and close; see [Milestone A](#eu-milestone-a-boundary). The [operator APIs](#eu-rtu-control-centre-apis) keep command results separate from physical evidence.

Sources: package `protocols/points.json` lines 12, 93–106; `services/iec104/src/rtu.c` lines 376–462; `services/iec104/README.md` lines 95–101, 155–162.
