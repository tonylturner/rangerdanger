`iec104cmd` is the range's IEC 104 client for a bounded protocol check, a JSONL observation stream and the command cases exercised by the labs. The Kali and engineering Dockerfiles build and install it at `/usr/local/bin/iec104cmd`. The vendor image does not install it.

Run the following from Kali's terminal. Under weak policy the RTU at `10.30.30.20:2404` is reachable; under improved policy a new Kali connection should be denied. Do not add an allow rule just to make a diagnostic succeed.

### Interrogate and observe

```sh
iec104cmd 10.30.30.20 gi --json
iec104cmd 10.30.30.20 monitor --json --hold 2
iec104cmd 10.30.30.20 monitor --json --hold 180 --gi-every 2
```

The first command checks GI completion; it does not print a point inventory. The monitor sends an initial GI and emits received point events. The longer recipe holds one connection and repeats GI, making it useful for the [established-session comparison](#eu-established-sessions).

`HOST[:PORT]` defaults to TCP/2404. Common address (`-ca`) and originator address (`-oa`) default to 1. These are protocol addressing fields, not authentication credentials.

Create the output directory if needed, then save a monitor without mixing its machine-readable output and diagnostics:

```sh
mkdir -p /home/kali/workspace
iec104cmd 10.30.30.20 monitor --json --hold 10 \
  > /home/kali/workspace/iec104-monitor.jsonl \
  2> /home/kali/workspace/iec104-monitor.log
```

Monitor events include `connected` (`local:null`), `asdu`, `gi_complete`, `held` and `link_down`. Step-position values contain a completed value and transient flag. Quality and timestamp evidence still need interpretation; receiving a number does not prove it is fresh and valid.

### Lab command scope

Only run state-changing cases during the corresponding lab step, with its stated process conditions and restoration plan:

```sh
iec104cmd 10.30.30.20 -ca 1 -oa 1 dc 3001 open -sbo --json
iec104cmd 10.30.30.20 dc 3001 close -sbo --json
```

Those are the breaker's configured double command, not a discovery technique. The labs also examine direct execute, expired select and mismatched execute: omitting `-sbo` sends direct execute; `-delay-ms 6000` and `-mismatch` imply SBO and exercise rejection. Do not sweep unknown IOAs with commands.

The current CLI additionally supports `rc <ioa> lower|raise` and `sc <ioa> on|off`. The configured OLTC commands are IOA 3301 for regulating steps and 3302 for mode; ON means auto and OFF manual. Use the operator lab workflow for these exercises rather than treating a CLI capability as permission to change equipment.

### Read the result, not just the exit code

Exit 0 means a one-shot operation succeeded or a monitor completed its hold. Exit 1 means operation failure; exit 2 means connection/STARTDT failure or usage error; exit 3 means a monitor lost its link. Preserve stderr to distinguish a denied connection from incorrect syntax.

For a command, CLI success requires positive ACT-CON and ACT-TERM. It does not independently match the feedback point the way the CC does. A CLI failure also cannot prove that a write never reached the device. Check [SBO and feedback](#iec104-sbo-commands), fresh operator observation and device evidence before retrying. `stopstart` manages data transfer on a link; it is not an electrical switching operation.
