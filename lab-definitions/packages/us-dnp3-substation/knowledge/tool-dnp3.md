The Kali box includes command-line tools for interacting with DNP3 outstations. `dnp3poll` reads data from an outstation, and `dnp3cmd` sends control commands.

### Reading Outstation Data with dnp3poll

To poll the relay outstation for its current data (a Class 0 integrity poll, FC01):

```bash
dnp3poll -a 1 -t 3s 10.40.40.20:20000
```

Flag breakdown:

- Put flags before the final target `IP:port` argument
- `-a 1` : outstation address (the device's DNP3 address)
- `-m 100` : master address, optional (defaults to 100, the Kali box)
- `-t 3s` : connection timeout, optional

### Outstation Addresses in the Lab

| Device | IP:Port | Address |
|--------|---------|---------|
| Relay | 10.40.40.20:20000 | 1 |
| Recloser | 10.40.40.21:20000 | 2 |
| Regulator | 10.40.40.22:20000 | 3 |
| RTAC | 10.30.30.20:20000 | 10 (read-only) |

### Sending Commands with dnp3cmd

`dnp3cmd` takes the target, the outstation address (`-a`), and a command verb. The `crob` verb sends a Control Relay Output Block to a binary output point; `analog` sends an analog output.

Trip the relay breaker (binary output index 0):

```bash
dnp3cmd 10.40.40.20:20000 -a 1 crob 0 trip
```

Disable auto-reclose on the recloser (binary output index 1):

```bash
dnp3cmd 10.40.40.21:20000 -a 2 crob 1 latch-off
```

Change the regulator tap position (analog output index 0):

```bash
dnp3cmd 10.40.40.22:20000 -a 3 analog 0 -16
```

CROB actions: `trip`, `close`, `latch-on`, `latch-off`, `pulse-on`, `pulse-off`.

### Control Modes: Direct Operate, No Ack, and SBO

By default `dnp3cmd` sends **Direct Operate** (FC05). Two flags select the other modes:

```bash
dnp3cmd 10.40.40.20:20000 -a 1 -sbo crob 0 trip       # Select-Before-Operate (FC03 then FC04)
dnp3cmd 10.40.40.20:20000 -a 1 -no-ack crob 0 trip    # Direct Operate No Ack (FC06)
```

- **Direct Operate** (FC05): single message, the outstation acts immediately and returns a status response.
- **Direct Operate No Ack** (FC06): same single-message control write, but the outstation sends no response. Use the lab instructions to observe the resulting device state.
- **Select-Before-Operate** (FC03/FC04): two steps. The master selects the control point, the outstation confirms, then the master issues operate. Safer for critical operations.

The lab simulators accept all three modes. Direct Operate is the more dangerous (and more commonly exploited) one. A real substation should use SBO for safety-critical controls, but many do not.

> Under the hardened policy, DNP3 commands from any source other than the RTAC (10.30.30.20) are blocked by containd at the firewall.