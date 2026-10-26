`tcpdump` captures raw packets on a network interface. In the lab, you run it on the containd firewall to capture traffic as it transits between zones. This is how you see exactly what the firewall is processing.

### Where to Run It

tcpdump runs in the firewall's Linux shell. Two ways in:

- **SSH:** `ssh -p 2222 containd@localhost` (password `containd`) opens a Linux bash prompt directly.
- **`fw-1` terminal:** opens at the `containd#` appliance CLI. Type `shell` to reach the Linux bash prompt.

### Find the Interface by Subnet

Docker assigns the firewall's `eth0`, `eth1`, ... names at boot, and the order differs between hosts. Never assume a fixed `ethN`. Look the device up from the zone's subnet instead:

```bash
FIELD_IF=$(ip -o -4 addr show to 10.40.40.0/24 | awk '{print $2}')
echo "$FIELD_IF"
```

This prints the device holding the firewall's `10.40.40.x` address (field_net) on this host. Run the lookup again in each new shell: the variable does not persist.

### Basic Capture on the Firewall

```bash
tcpdump -i "$FIELD_IF" -w /tmp/field_capture.pcap
```

- `-i "$FIELD_IF"` : capture on the field_net interface (lan2)
- `-w /tmp/field_capture.pcap` : write raw packets to file

Press Ctrl+C to stop. You can then copy the file out or analyze it with tshark.

### BPF Filter Syntax

BPF (Berkeley Packet Filter) expressions limit what tcpdump captures:

```bash
# Only Modbus TCP traffic
tcpdump -i "$FIELD_IF" port 502

# Only DNP3 traffic
tcpdump -i "$FIELD_IF" port 20000

# Traffic from a specific host
tcpdump -i "$FIELD_IF" host 10.40.40.20

# Modbus traffic to the relay specifically
tcpdump -i "$FIELD_IF" host 10.40.40.20 and port 502

# Traffic from the Kali box to any field device, on every interface
tcpdump -i any -nn src host 10.10.10.50 and dst net 10.40.40.0/24
```

The last example uses `-i any` because the Kali box sits in enterprise_net. A packet the firewall forwards shows up twice: `In` on the enterprise interface and `Out` on the field interface. A packet the firewall drops shows only the `In` line. Capturing on the field interface alone shows only the forwarded traffic.

### Common BPF Operators

- `host 10.40.40.20` : matches source or destination
- `src host` / `dst host` : matches only source or destination
- `port 502` : matches source or destination port
- `and`, `or`, `not` : boolean combinators
- `net 10.40.40.0/24` : matches an entire subnet

### Useful Flags

| Flag | Purpose |
|------|---------|
| `-i "$FIELD_IF"` | Interface to capture on (looked up from its subnet) |
| `-i any` | Capture on every interface; each line names the device and direction |
| `-w file.pcap` | Write to file (for later analysis with tshark) |
| `-c 100` | Stop after 100 packets |
| `-n` | Do not resolve hostnames (faster) |
| `-v` / `-vv` | Verbose output |
| `-X` | Print packet contents in hex and ASCII |

### Which Interface Is Which?

Each zone has a fixed subnet and a fixed containd logical interface name. Only the `ethN` device name changes between hosts:

| Zone | Subnet | containd interface |
|------|--------|--------------------|
| enterprise_net | 10.10.10.0/24 | wan |
| vendor_net | 10.20.20.0/24 | dmz |
| ot_ops_net | 10.30.30.0/24 | lan1 |
| field_net | 10.40.40.0/24 | lan2 |
| mgmt_net | 10.99.99.0/24 | lan3 (management, out of band) |

To list the current device for every zone on this host:

```bash
for net in 10.10.10.0/24 10.20.20.0/24 10.30.30.0/24 10.40.40.0/24 10.99.99.0/24; do
  echo "$net -> $(ip -o -4 addr show to "$net" | awk '{print $2}')"
done
```

> Capture on the interface closest to the traffic you care about. If you want to see attack traffic hitting field devices, capture on the device that holds 10.40.40.x (field_net). If you want to see what leaves the enterprise zone, capture on the device that holds 10.10.10.x. When you are not sure, use `-i any` with a subnet or port filter.
