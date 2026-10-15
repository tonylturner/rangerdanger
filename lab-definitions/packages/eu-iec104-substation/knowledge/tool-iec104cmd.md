These examples describe the slice; they have not been executed against a running range as part of this article.

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

The direct, delayed-expired and mismatched command attempts should reject. Normal outcomes still depend on policy and device interlocks. HOST[:PORT] defaults to port 2404; CA and OA default to 1. `-delay-ms` and `-mismatch` imply SBO. Generic `sc` support does not mean this RTU supports single commands.

One-shot JSON emits a result, not point objects. Monitor emits JSONL events: connected (`local:null`), asdu, gi_complete, held and link_down. Logs go to stderr. Exit 0 means success/held, 1 operation failure, 2 connection/STARTDT failure or usage, and 3 monitor link loss.

CLI success is not the CC's physical-completion proof: matching feedback is not enforced as it is in the CC. Use [SBO evidence](#iec104-sbo-commands) and operator state, not only an exit status. STOPDT/STARTDT manages transfer, not switching.

Sources: `services/iec104/src/iec104cmd.c` lines 33–60, 214–216, 243–347, 364–445; `services/iec104/README.md` lines 155–162.
