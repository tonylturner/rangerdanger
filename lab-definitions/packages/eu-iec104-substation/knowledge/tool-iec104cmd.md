```sh
iec104cmd 10.30.30.20 gi --json
iec104cmd 10.30.30.20 monitor --json --hold 2
iec104cmd 10.30.30.20 monitor --json --hold 180 --gi-every 2
iec104cmd 10.30.30.20 -ca 1 -oa 1 dc 3001 open -sbo --json
iec104cmd 10.30.30.20 dc 3001 close -sbo --json
iec104cmd 10.30.30.20 dc 3001 close --json
iec104cmd 10.30.30.20 dc 3001 close -delay-ms 6000 --json
iec104cmd 10.30.30.20 dc 3001 close -mismatch --json
iec104cmd 10.30.30.20 stopstart --json
```

The RTU rejects direct, expired and mismatched command attempts. Other outcomes depend on policy and device interlocks. `HOST[:PORT]` defaults to port 2404. CA and OA default to 1. `-delay-ms` and `-mismatch` imply SBO. Generic `sc` support does not mean this RTU supports single commands.

One-shot JSON emits a result, not point objects. Monitor emits JSONL events: connected (`local:null`), asdu, gi_complete, held and link_down. Logs go to stderr. Exit 0 means the operation succeeded or the monitor completed its hold. Exit 1 means the operation failed. Exit 2 means connection or STARTDT failure, or incorrect usage. Exit 3 means the monitor lost its link.

For a command, exit 0 means positive activation confirmation (ACT-CON) and activation termination (ACT-TERM) arrived. A missing ACT-TERM makes the CLI exit 1. The CLI does not check the breaker's matching feedback the way the Control Centre does. Use [SBO evidence](#iec104-sbo-commands) and operator state to check the physical result. STOPDT/STARTDT manages transfer, not switching.
