Control authority has several layers: network reachability, protocol sequencing, device operating mode, physical interlocks and organizational permission. Passing one layer does not imply passing the others. This range deliberately makes those layers visible instead of equating “connected” with “authorized.”

### Who requests, who decides

The CC sends IEC 104 commands to the RTU. The RTU maps a configured command IOA to a field-device Modbus write. The device model then checks authority and interlocks atomically. The process solver supplies electrical measurements; it is not another student switching console.

| Function | IEC 104 command | Device decision |
|---|---|---|
| Breaker open/close | IOA 3001, double command | Remote authority, lockout and close conditions |
| RMU1 / RMU2 | IOA 3101 / 3201, double command | Remote, earth state, valid measurements and switching interlocks |
| OLTC manual step | IOA 3301, regulating step | Remote + manual, idle, energized, unblocked, within limits |
| OLTC mode | IOA 3302, single command | Remote; ON means auto, OFF means manual |

The PLC follows a different path: Modbus to OLTC holding register 1, requesting an adjacent absolute tap through the automatic-control handshake. It does not send the CC's manual-step command.

### Local, remote, auto and manual differ

Local authority refuses remote control; it does not mean the device is electrically isolated. An open way may still have a live source or section side. Remote authority allows evaluation of a request, not unconditional execution.

OLTC auto/manual selects which remote control path is permitted. A manual raise is refused in auto; an automatic PLC request is not honored in manual. Switching to manual during motion does not cancel the already accepted step: the step in flight completes.

For the RMUs, a close is refused if both sides are live or measurements are invalid. Opening is refused while valid measurements show fault current, and also when measurements are invalid. A load-break way is not a breaker and must not be assumed able to interrupt a fault.

### Sequencing is not identity

All configured IEC 104 controls use select-before-operate. The RTU checks the selected command, timeout and execute value. This reduces accidental or mismatched execution, but the common address, IOA and originator address are not passwords.

Under weak policy, a reachable second master can speak the same valid sequence. Improved policy limits which source may connect; it does not inspect the business purpose of the allowed master's command. See [trusted-host misuse](#eu-trusted-host-misuse).

### Interpret the result precisely

Positive activation confirmation for the execute means a write was acknowledged, not necessarily that motion has finished. Confirmation of the select alone does not mean any write occurred. The RTU waits for fresh matching feedback before successful termination. The CC reports `completed`, `rejected`, `failed` or `unknown`; a lost write response must not become an automatic retry.

For an uncertain command, inspect fresh feedback and the appropriate audit before deciding what to do next. Repeating a tap request because the display timed out can cause an additional operation. The [operator APIs](#eu-rtu-control-centre-apis) expose evidence, not an excuse to bypass the command path.

Finally, lab step actions can set local/remote state, block equipment or introduce faults through management. Those actions create exercise conditions. They are not proof that an operator command traversed IEC 104 and Modbus, and they must remain distinct in the acceptance record.
