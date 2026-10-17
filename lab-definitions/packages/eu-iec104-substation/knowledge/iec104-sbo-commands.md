Select-before-operate (SBO) separates reserving an intended operation from executing it. It reduces accidental or conflicting control sequences, but it is not identity authentication or a safety case. An allowed client can still request a harmful operation that satisfies the sequence. Network source restrictions, device interlocks and operating procedures address different parts of that risk.

### Commands implemented here

The package has five SBO command points. Breaker 3001 and RMUs 3101/3201 use double commands: 1 open, 2 close. OLTC 3301 uses regulating steps: 1 lower, 2 raise. OLTC mode 3302 uses a single command: 0 manual, 1 auto. The RTU translates mode to Modbus HR2 values 1/2; the IEC single-command value is not the raw register encoding.

Selection expires after 5000 ms. Execute must match the selected peer, command, value and qualifier. Only qualifier 0 is accepted. Direct execute, expired or mismatched selection, and competing work receive negative confirmation. One execute is in flight at a time.

### Confirmation is not completion

Before executing, the RTU requires fresh good start feedback from a poll begun after select. It writes the target once using Modbus FC6. A device exception produces negative ACT-CON. A write sent without a response is ambiguous: no positive confirmation is invented and the RTU records `unknown`, without retrying the write.

An acknowledged write produces positive ACT-CON, followed by a wait for fresh matching feedback. For a double command, position must match. For a regulating step, completed position must equal start ±1, transient false and good quality. For mode, the auto indication must match the requested single-command state. Matching feedback produces COT 11 return information and positive ACT-TERM. Expiry produces negative ACT-TERM without COT 11. The feedback budget is five seconds for regulating steps and the device timeout for other commands.

The CC also refuses before select if its feedback is missing, IV, NT or older than the configured stale threshold. This admission refusal is distinct from a command that reached the RTU and was refused there.

### Interpret results without automatic retries

The CC reports:

| Result | Meaning |
| --- | --- |
| `completed` | Confirmed execution, positive termination and matching fresh return information |
| `rejected` | Negative select or execute ACT-CON |
| `failed` | Negative ACT-TERM after acknowledged execution |
| `unknown` | Required confirmation, termination or feedback missing; outcome ambiguous |

Read the result's reason and nullable phase times. `unknown` can also describe a link or selection timeout before execute was sent; it does not prove a physical action happened. Do not interpret `failed` as proof that execute was never sent.

### Observe one controlled attempt

Save the CC journal cursor and starting feedback. Use the **Control Centre** selected-device faceplate or [API](#eu-rtu-control-centre-apis) for one operation, then preserve the result id, selected/executed/confirmation/termination times and feedback object. Correlate them with the capture and physical audit. Do not infer causality solely from the final position: a no-op open may complete without movement, or a local trip may change position independently.

### Limits

Never automatically retry an ambiguous execute. Another write is another operation, especially for [tap steps](#eu-iec104-step-telemetry). SBO does not stop a compromised trusted CC from issuing correctly sequenced requests, nor does it prove protocol-aware firewall filtering. Field authority and interlocks can refuse requests that passed every network and IEC 104 check.

Implementation: `services/iec104/src/rtu.c`, `cc_command.c`, `cc_state.c`; package `protocols/points.json`.
