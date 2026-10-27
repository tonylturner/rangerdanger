# US OpenPLC seed

The image bakes and compiles this local-only feeder close-permissive demo.
At boot it verifies the seed hash, installs it, confirms that no Modbus
master devices are configured, and requires the runtime heartbeat to advance.
A top-level ST file mounted under `/workdir` overrides the baked seed.

The program uses illustrative local conditions only. It has no field I/O,
does not poll other hosts, and is not protection or process-control logic.
OpenPLC exposes its own Modbus slave:

| Holding register | ST variable | Meaning |
|---|---|---|
| 1024 | `%MW0` | Local close request (write `1` to request) |
| 1025 | `%MW1` | Status bits: open, bus OK, protection healthy, permissive, output on |
| 1026 | `%MW2` | Demonstration close output (energized only while permitted) |
| 1027 | `%MW3` | Heartbeat, incremented every ten 50 ms scans |
