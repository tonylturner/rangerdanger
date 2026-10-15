The implemented breaker command is IOA 3001: DCS 1 opens and DCS 2 closes. Select-before-operate (SBO) creates a selection that expires after 5000 ms. Execute must match the selected peer, command, value and qualifier. The RTU rejects direct, expired and mismatched execute requests.

The RTU sends Modbus FC6 once. A positive echo leads to ACT-CON, COT 11 feedback and ACT-TERM. The CC additionally requires matching feedback after execute confirmation before declaring completion.

A `rejected` command received a negative select or execute confirmation. `failed` means execute was never sent. The link may have been lost first, or select confirmation may have been missing. `unknown` means execute was sent but its outcome is unclear. Confirmation, termination or matching feedback may be missing. Do not retry an ambiguous command automatically. Another execute is another operation. The CLI's result alone does not prove that the breaker moved.

SBO is neither identity authorization nor firewall deep packet inspection. An allowed CC can open and close the breaker. See [port- and source-based rules](#eu-port-source-boundary). The [operator APIs](#eu-rtu-control-centre-apis) keep command results separate from physical evidence.
