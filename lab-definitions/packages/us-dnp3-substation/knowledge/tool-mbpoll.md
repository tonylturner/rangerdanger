`mbpoll` is a command-line Modbus master (client) that lets you read from and write to Modbus TCP devices. In the lab, it is pre-installed on the Kali box at 10.10.10.50 and is your primary tool for interacting with field device simulators over Modbus.

> **Mode flag is optional.** `mbpoll` defaults to TCP whenever the target is an IP address, so `-m tcp` is redundant in this lab. Older write-ups elsewhere may include it - it's harmless, just noise. The lab YAMLs standardize on the no-mode form to match this article.

### Reading Holding Registers (FC03)

To read holding registers from a device:

```bash
mbpoll -a 1 -r 1 -c 5 -t 4 -1 10.40.40.20
```

Flag breakdown:

- `-a 1` : Modbus slave address (1 is typical for TCP devices)
- `-r 1` : starting register number
- `-c 5` : number of registers to read
- `-t 4` : register type (4 = holding registers, FC03)
- `-1` : single poll, then exit (without this, mbpoll polls continuously)
- Last argument: target IP address

### Reading Coils (FC01)

```bash
mbpoll -a 1 -r 1 -c 4 -t 0 -1 10.40.40.20
```

- `-t 0` : coil type (FC01)

### Writing a Single Coil (FC05)

To write a coil value (e.g., trip the relay breaker):

```bash
mbpoll -a 1 -r 1 -t 0 -1 10.40.40.20 1
```

The value at the end (1 or 0) sets the coil ON or OFF.

### Writing a Single Register (FC06)

To write a holding register (e.g., set regulator tap position):

```bash
mbpoll -a 1 -r 1 -t 4 -1 10.40.40.22 5
```

The value at the end is written to the register.

### Common Lab Targets

| Device | IP | What to Read/Write |
|--------|----|--------------------|
| Relay | 10.40.40.20 | Breaker coil, trip count, lockout |
| Recloser | 10.40.40.21 | Reclose-enabled coil, shot count |
| Regulator | 10.40.40.22 | Tap position register |

> When the hardened firewall policy is active, `mbpoll` commands from the Kali box will time out because containd blocks Modbus traffic from the enterprise zone to field devices.