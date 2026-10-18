# EU OpenPLC seed

`Dockerfile.openplc-eu` bakes these files into `/opt/openplc-eu`. Do not
mount a workdir or a database over the image. Every boot checks the seed
manifest, compiles a fresh copy of `tap-control.st`, installs and reads back
the database configuration, then starts the engineering web UI and runtime.
The manifest's SHA-256 is logged and recorded in
`/var/lib/openplc-eu-seed.sha256`. A bounded startup check reads the actual
Modbus status block and requires its heartbeat to advance.

The Docker build also compiles the ST, so syntax errors fail the image build.

The pinned upstream image is **linux/amd64 only**. Build with:

```sh
docker build --platform linux/amd64 -f Dockerfile.openplc-eu -t rangerdanger-openplc-eu .
shellcheck scripts/openplc-eu-entrypoint.sh
```

## Wire map

The only master device is unit 1, `10.40.40.23:502`, polled every 100 ms:

| Operation | Device address | ST location |
|---|---|---|
| FC4, 15 registers | IR0..14 | `%IW100..114` |
| FC16, one register | HR1 | `%QW100` |

The runtime's slave FC3 exposes `%QW100` at 100 and `%MW0..5` at
1024..1029. Status registers are flags (running, input valid, control
permitted), 20400 V setpoint, 10 per-mille deadband, configured delay
seconds (30), last demand (0 none / 1 lower / 2 raise), and a
wrapping heartbeat incremented every ten 50 ms scans.

Input validity requires an observed change of the device heartbeat, no
two-second heartbeat timeout, IR2=1, and energisation. Permission additionally
requires remote, auto, idle, and not effectively blocked (manual OR solved
overcurrent, IR1 bit 5). A generation change or an
unarmed result cancels the demand, writing zero to allow device-side arming.
Never infer freshness from retained `%IW` values alone.

An out-of-band demand must persist in the same direction for the configured 30 seconds. The TON preset is derived from `%MW3`,
and thresholds derive from `%MW1` ± `%MW1` × `%MW2` / 1000
(20196–20604 V). `Remaining` is internal and is not telemetered.
An outstanding absolute request is not recalculated if voltage changes
direction; it is cleared on inhibition, no demand, or completion. A new
delay follows the step. Requests are zero at boot and whenever inhibited.
Inward requests at either tap limit remain allowed.

For headless integration checks, isolate a fake OLTC from any live range.
Assert actual FC4 and FC16 traffic, FC3 status/heartbeat, timing, inhibition,
and zero-before-request arming. A Modbus listener alone is not boot evidence.
