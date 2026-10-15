`tcpdump` captures raw packets on a network interface. In the lab, you run it on the containd firewall (via SSH on port 2222) to capture traffic as it transits between zones. This is how you see exactly what the firewall is processing.

### Basic Capture on the Firewall

SSH into the firewall and run:

```bash
ssh -p 2222 containd@localhost
tcpdump -i eth3 -w /tmp/field_capture.pcap
```

- `-i eth3` : capture on the field_net interface (lan2)
- `-w /tmp/field_capture.pcap` : write raw packets to file

Press Ctrl+C to stop. You can then copy the file out or analyze it with tshark.

### BPF Filter Syntax

BPF (Berkeley Packet Filter) expressions limit what tcpdump captures:

```bash
# Only Modbus TCP traffic
tcpdump -i eth3 port 502

# Only DNP3 traffic
tcpdump -i eth3 port 20000

# Traffic from a specific host
tcpdump -i eth3 host 10.40.40.20

# Modbus traffic to the relay specifically
tcpdump -i eth3 host 10.40.40.20 and port 502

# Traffic from the Kali box to any field device
tcpdump -i eth3 src host 10.10.10.50
```

### Common BPF Operators

- `host 10.40.40.20` : matches source or destination
- `src host` / `dst host` : matches only source or destination
- `port 502` : matches source or destination port
- `and`, `or`, `not` : boolean combinators
- `net 10.40.40.0/24` : matches an entire subnet

### Useful Flags

| Flag | Purpose |
|------|---------|
| `-i eth3` | Interface to capture on |
| `-w file.pcap` | Write to file (for later analysis with tshark) |
| `-c 100` | Stop after 100 packets |
| `-n` | Do not resolve hostnames (faster) |
| `-v` / `-vv` | Verbose output |
| `-X` | Print packet contents in hex and ASCII |

### Which Interface Is Which?

On the containd firewall, interfaces map to zones:

| Interface | Zone | Subnet |
|-----------|------|--------|
| eth0 (wan) | enterprise_net | 10.10.10.0/24 |
| eth1 (dmz) | vendor_net | 10.20.20.0/24 |
| eth2 (lan1) | ot_ops_net | 10.30.30.0/24 |
| eth3 (lan2) | field_net | 10.40.40.0/24 |

> Capture on the interface closest to the traffic you care about. If you want to see attack traffic hitting field devices, capture on eth3 (field_net). If you want to see what leaves the enterprise zone, capture on eth0.