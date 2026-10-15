`nmap` is the standard network scanner for host discovery and port enumeration. In the lab, it is pre-installed on the Kali box and is used in the reconnaissance phase of attack exercises to discover what is reachable from the enterprise zone.

### Basic Host Discovery

To find live hosts on a subnet:

```bash
nmap -sn 10.40.40.0/24
```

The `-sn` flag does a ping sweep without port scanning. Fast way to see which field devices are up.

### Port Scanning a Specific Host

```bash
nmap -sT -p 502,8080,20000 10.40.40.20
```

- `-sT` : TCP connect scan (full three-way handshake, reliable)
- `-p 502,8080,20000` : scan only the ports used by lab simulators

### Scanning for ICS Protocols Across a Zone

```bash
nmap -sT -p 502,20000 10.40.40.0/24
```

This finds all hosts on the field network with open Modbus (502) or DNP3 (20000) ports. In a real engagement, this is how you identify ICS devices on a network segment.

### Service Version Detection

```bash
nmap -sV -p 502,8080,20000 10.40.40.20
```

The `-sV` flag probes open ports to identify the running service and version. Useful for fingerprinting, though the lab simulators may not return detailed version strings.

### Scanning Through the Firewall

The key observation in the lab exercises is how nmap results change between firewall policies:

- **Weak policy**: scanning from the Kali box (10.10.10.50) shows ports 502, 8080, and 20000 open on field devices
- **Hardened policy**: the same scan shows ports filtered or unreachable because containd blocks enterprise-to-field traffic

This is a concrete, visible demonstration of what segmentation does. The nmap output is the proof.

### Common Flags Reference

| Flag | Purpose |
|------|---------|
| `-sn` | Ping sweep, no port scan |
| `-sT` | TCP connect scan |
| `-sS` | TCP SYN scan (requires root, stealthier) |
| `-sV` | Service version detection |
| `-p 502,20000` | Specific ports |
| `-p-` | All 65535 ports (slow) |
| `--open` | Only show open ports in output |
| `-oN file.txt` | Save output to file |

> Start reconnaissance with a targeted port scan (`-p 502,8080,20000`) rather than scanning all ports. You already know what the lab simulators expose, and targeted scans are much faster.