import type { Section } from "../knowledge-types";

export const toolReferences: Section =
  /* ====== Command References & Lab Tools ====== */
  {
    heading: "Command References & Lab Tools",
    description:
      "Quick references for the CLI tools the exercises use: mbpoll, dnp3poll, dnp3cmd, tshark, tcpdump, curl, nmap.",
    icon: "wrench",
    accent: "amber",
    articles: [
      {
        id: "tool-mbpoll",
        title: "mbpoll - Modbus TCP Command-Line Tool",
        body: `\`mbpoll\` is a command-line Modbus master (client) that lets you read from and write to Modbus TCP devices. In the lab, it is pre-installed on the Kali box at 10.10.10.50 and is your primary tool for interacting with field device simulators over Modbus.

> **Mode flag is optional.** \`mbpoll\` defaults to TCP whenever the target is an IP address, so \`-m tcp\` is redundant in this lab. Older write-ups elsewhere may include it - it's harmless, just noise. The lab YAMLs standardize on the no-mode form to match this article.

### Reading Holding Registers (FC03)

To read holding registers from a device:

\`\`\`bash
mbpoll -a 1 -r 1 -c 5 -t 4 -1 10.40.40.20
\`\`\`

Flag breakdown:

- \`-a 1\` : Modbus slave address (1 is typical for TCP devices)
- \`-r 1\` : starting register number
- \`-c 5\` : number of registers to read
- \`-t 4\` : register type (4 = holding registers, FC03)
- \`-1\` : single poll, then exit (without this, mbpoll polls continuously)
- Last argument: target IP address

### Reading Coils (FC01)

\`\`\`bash
mbpoll -a 1 -r 1 -c 4 -t 0 -1 10.40.40.20
\`\`\`

- \`-t 0\` : coil type (FC01)

### Writing a Single Coil (FC05)

To write a coil value (e.g., trip the relay breaker):

\`\`\`bash
mbpoll -a 1 -r 1 -t 0 -1 10.40.40.20 1
\`\`\`

The value at the end (1 or 0) sets the coil ON or OFF.

### Writing a Single Register (FC06)

To write a holding register (e.g., set regulator tap position):

\`\`\`bash
mbpoll -a 1 -r 1 -t 4 -1 10.40.40.22 5
\`\`\`

The value at the end is written to the register.

### Common Lab Targets

| Device | IP | What to Read/Write |
|--------|----|--------------------|
| Relay | 10.40.40.20 | Breaker coil, trip count, lockout |
| Recloser | 10.40.40.21 | Reclose-enabled coil, shot count |
| Regulator | 10.40.40.22 | Tap position register |

> When the hardened firewall policy is active, \`mbpoll\` commands from the Kali box will time out because containd blocks Modbus traffic from the enterprise zone to field devices.`,
      },
      {
        id: "tool-dnp3",
        title: "dnp3poll & dnp3cmd - DNP3 Tools",
        body: `The Kali box includes command-line tools for interacting with DNP3 outstations. \`dnp3poll\` reads data from an outstation, and \`dnp3cmd\` sends control commands.

### Reading Outstation Data with dnp3poll

To poll the relay outstation for its current data (a Class 0 integrity poll, FC01):

\`\`\`bash
dnp3poll 10.40.40.20:20000 -a 1
\`\`\`

Flag breakdown:

- First argument: target \`IP:port\`
- \`-a 1\` : outstation address (the device's DNP3 address)
- \`-m 100\` : master address, optional (defaults to 100, the Kali box)
- \`-t 3s\` : connection timeout, optional

### Outstation Addresses in the Lab

| Device | IP:Port | Address |
|--------|---------|---------|
| Relay | 10.40.40.20:20000 | 1 |
| Recloser | 10.40.40.21:20000 | 2 |
| Regulator | 10.40.40.22:20000 | 3 |
| RTAC | 10.30.30.20:20000 | 10 (read-only) |

### Sending Commands with dnp3cmd

\`dnp3cmd\` takes the target, the outstation address (\`-a\`), and a command verb. The \`crob\` verb sends a Control Relay Output Block to a binary output point; \`analog\` sends an analog output.

Trip the relay breaker (binary output index 0):

\`\`\`bash
dnp3cmd 10.40.40.20:20000 -a 1 crob 0 trip
\`\`\`

Disable auto-reclose on the recloser (binary output index 1):

\`\`\`bash
dnp3cmd 10.40.40.21:20000 -a 2 crob 1 latch-off
\`\`\`

Change the regulator tap position (analog output index 0):

\`\`\`bash
dnp3cmd 10.40.40.22:20000 -a 3 analog 0 -16
\`\`\`

CROB actions: \`trip\`, \`close\`, \`latch-on\`, \`latch-off\`, \`pulse-on\`, \`pulse-off\`.

### Control Modes: Direct Operate, No Ack, and SBO

By default \`dnp3cmd\` sends **Direct Operate** (FC05). Two flags select the other modes:

\`\`\`bash
dnp3cmd 10.40.40.20:20000 -a 1 -sbo crob 0 trip       # Select-Before-Operate (FC03 then FC04)
dnp3cmd 10.40.40.20:20000 -a 1 -no-ack crob 0 trip    # Direct Operate No Ack (FC06)
\`\`\`

- **Direct Operate** (FC05): single message, the outstation acts immediately and returns a status response.
- **Direct Operate No Ack** (FC06): same single-message control write, but the outstation sends no response. Quieter for an attacker, and an evasion path against a filter that only lists FC05 - a correct DPI rule covers both.
- **Select-Before-Operate** (FC03/FC04): two steps. The master selects the control point, the outstation confirms, then the master issues operate. Safer for critical operations.

The lab simulators accept all three modes. Direct Operate is the more dangerous (and more commonly exploited) one. A real substation should use SBO for safety-critical controls, but many do not.

> Under the hardened policy, DNP3 commands from any source other than the RTAC (10.30.30.20) are blocked by containd at the firewall.`,
      },
      {
        id: "tool-tshark",
        title: "tshark - Network Protocol Analysis",
        body: `\`tshark\` is the command-line version of Wireshark. In the lab, you use it to analyze network traffic and verify what protocols are flowing between zones. It is available on the Kali box and (depending on configuration) on the firewall itself.

### Protocol Volume Summary (io,stat)

To see a summary of protocol traffic over time:

\`\`\`bash
tshark -r capture.pcap -q -z io,stat,10,modbus,dnp3
\`\`\`

This gives you a table showing packet counts for Modbus and DNP3 traffic in 10-second intervals. Useful for seeing traffic patterns and spotting when attack traffic starts.

### Host Pair Summary (conv,ip)

To see which IP addresses are talking to each other:

\`\`\`bash
tshark -r capture.pcap -q -z conv,ip
\`\`\`

This produces a table of all IP conversation pairs with packet counts and byte totals. You can quickly identify which hosts are generating the most traffic and spot unexpected communication paths (like enterprise hosts talking directly to field devices).

### TCP Connection Detail (conv,tcp)

To see individual TCP connections:

\`\`\`bash
tshark -r capture.pcap -q -z conv,tcp
\`\`\`

This shows source/destination IP:port pairs for every TCP session. Useful for confirming which specific services are being accessed (port 502 = Modbus, port 20000 = DNP3, port 8080 = HTTP REST).

### Display Filters

tshark supports Wireshark display filter syntax for narrowing results:

\`\`\`bash
# Only Modbus traffic
tshark -r capture.pcap -Y "modbus"

# Modbus writes only (FC05 or FC06)
tshark -r capture.pcap -Y "modbus.func_code == 5 || modbus.func_code == 6"

# DNP3 traffic to the relay
tshark -r capture.pcap -Y "dnp3 && ip.dst == 10.40.40.20"

# All traffic from the Kali box
tshark -r capture.pcap -Y "ip.src == 10.10.10.50"
\`\`\`

### Live Capture

To capture live traffic on an interface:

\`\`\`bash
tshark -i eth0 -w capture.pcap
\`\`\`

Add \`-f "port 502"\` as a BPF capture filter to limit what gets written to the file. Use \`-c 100\` to stop after 100 packets.

> The three views (io,stat, conv,ip, conv,tcp) give you a quick top-down picture of network activity without scrolling through individual packets. Start there before diving into packet-level analysis.`,
      },
      {
        id: "tool-tcpdump",
        title: "tcpdump - Packet Capture",
        body: `\`tcpdump\` captures raw packets on a network interface. In the lab, you run it on the containd firewall (via SSH on port 2222) to capture traffic as it transits between zones. This is how you see exactly what the firewall is processing.

### Basic Capture on the Firewall

SSH into the firewall and run:

\`\`\`bash
ssh -p 2222 containd@localhost
tcpdump -i eth3 -w /tmp/field_capture.pcap
\`\`\`

- \`-i eth3\` : capture on the field_net interface (lan2)
- \`-w /tmp/field_capture.pcap\` : write raw packets to file

Press Ctrl+C to stop. You can then copy the file out or analyze it with tshark.

### BPF Filter Syntax

BPF (Berkeley Packet Filter) expressions limit what tcpdump captures:

\`\`\`bash
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
\`\`\`

### Common BPF Operators

- \`host 10.40.40.20\` : matches source or destination
- \`src host\` / \`dst host\` : matches only source or destination
- \`port 502\` : matches source or destination port
- \`and\`, \`or\`, \`not\` : boolean combinators
- \`net 10.40.40.0/24\` : matches an entire subnet

### Useful Flags

| Flag | Purpose |
|------|---------|
| \`-i eth3\` | Interface to capture on |
| \`-w file.pcap\` | Write to file (for later analysis with tshark) |
| \`-c 100\` | Stop after 100 packets |
| \`-n\` | Do not resolve hostnames (faster) |
| \`-v\` / \`-vv\` | Verbose output |
| \`-X\` | Print packet contents in hex and ASCII |

### Which Interface Is Which?

On the containd firewall, interfaces map to zones:

| Interface | Zone | Subnet |
|-----------|------|--------|
| eth0 (wan) | enterprise_net | 10.10.10.0/24 |
| eth1 (dmz) | vendor_net | 10.20.20.0/24 |
| eth2 (lan1) | ot_ops_net | 10.30.30.0/24 |
| eth3 (lan2) | field_net | 10.40.40.0/24 |

> Capture on the interface closest to the traffic you care about. If you want to see attack traffic hitting field devices, capture on eth3 (field_net). If you want to see what leaves the enterprise zone, capture on eth0.`,
      },
      {
        id: "tool-curl",
        title: "curl - HTTP REST API Interaction",
        body: `Every field device simulator in the lab exposes an HTTP REST API on **port 8080**. \`curl\` is the simplest way to read device state, send commands, and check health. It is available on the Kali box and most other lab containers.

### Reading Device State

\`\`\`bash
# Relay state
curl -s http://10.40.40.20:8080/api/state | jq .

# Recloser state
curl -s http://10.40.40.21:8080/api/state | jq .

# Regulator state
curl -s http://10.40.40.22:8080/api/state | jq .

# RTAC aggregated state (all devices)
curl -s http://10.30.30.20:8080/api/state | jq .
\`\`\`

Pipe through \`jq\` for readable JSON output. The \`-s\` flag silences the progress bar.

### Sending Commands

Commands go via POST to \`/api/command\`:

\`\`\`bash
# Trip the relay breaker
curl -s -X POST http://10.40.40.20:8080/api/command \\
  -H "Content-Type: application/json" \\
  -d '{"action": "trip"}'

# Close the relay breaker
curl -s -X POST http://10.40.40.20:8080/api/command \\
  -H "Content-Type: application/json" \\
  -d '{"action": "close"}'

# Disable recloser auto-reclose
curl -s -X POST http://10.40.40.21:8080/api/command \\
  -H "Content-Type: application/json" \\
  -d '{"action": "disable-reclose"}'

# Set regulator tap position
curl -s -X POST http://10.40.40.22:8080/api/command \\
  -H "Content-Type: application/json" \\
  -d '{"action": "set-tap", "value": 10}'
\`\`\`

### Health Checks

\`\`\`bash
curl -s http://10.40.40.20:8080/api/health
\`\`\`

Returns a simple status response. Useful for verifying a device is running before trying protocol-level interaction.

### Audit Log

\`\`\`bash
curl -s http://10.40.40.20:8080/api/audit | jq .
\`\`\`

Returns the command audit trail showing all commands the device has received. This is useful for confirming that your Modbus or DNP3 commands actually reached the device and what effect they had.

### When to Use curl vs. Modbus/DNP3

The HTTP API is convenient for quick checks and is not subject to the same firewall rules as industrial protocols. However, the exercises focus on Modbus and DNP3 because those are the protocols used in real substations. Use \`curl\` for:

- Verifying device state before and after attacks
- Confirming that commands sent via Modbus/DNP3 had the expected effect
- Quick troubleshooting when something is not working as expected`,
      },
      {
        id: "tool-nmap",
        title: "nmap - Port Scanning & Reconnaissance",
        body: `\`nmap\` is the standard network scanner for host discovery and port enumeration. In the lab, it is pre-installed on the Kali box and is used in the reconnaissance phase of attack exercises to discover what is reachable from the enterprise zone.

### Basic Host Discovery

To find live hosts on a subnet:

\`\`\`bash
nmap -sn 10.40.40.0/24
\`\`\`

The \`-sn\` flag does a ping sweep without port scanning. Fast way to see which field devices are up.

### Port Scanning a Specific Host

\`\`\`bash
nmap -sT -p 502,8080,20000 10.40.40.20
\`\`\`

- \`-sT\` : TCP connect scan (full three-way handshake, reliable)
- \`-p 502,8080,20000\` : scan only the ports used by lab simulators

### Scanning for ICS Protocols Across a Zone

\`\`\`bash
nmap -sT -p 502,20000 10.40.40.0/24
\`\`\`

This finds all hosts on the field network with open Modbus (502) or DNP3 (20000) ports. In a real engagement, this is how you identify ICS devices on a network segment.

### Service Version Detection

\`\`\`bash
nmap -sV -p 502,8080,20000 10.40.40.20
\`\`\`

The \`-sV\` flag probes open ports to identify the running service and version. Useful for fingerprinting, though the lab simulators may not return detailed version strings.

### Scanning Through the Firewall

The key observation in the lab exercises is how nmap results change between firewall policies:

- **Weak policy**: scanning from the Kali box (10.10.10.50) shows ports 502, 8080, and 20000 open on field devices
- **Hardened policy**: the same scan shows ports filtered or unreachable because containd blocks enterprise-to-field traffic

This is a concrete, visible demonstration of what segmentation does. The nmap output is the proof.

### Common Flags Reference

| Flag | Purpose |
|------|---------|
| \`-sn\` | Ping sweep, no port scan |
| \`-sT\` | TCP connect scan |
| \`-sS\` | TCP SYN scan (requires root, stealthier) |
| \`-sV\` | Service version detection |
| \`-p 502,20000\` | Specific ports |
| \`-p-\` | All 65535 ports (slow) |
| \`--open\` | Only show open ports in output |
| \`-oN file.txt\` | Save output to file |

> Start reconnaissance with a targeted port scan (\`-p 502,8080,20000\`) rather than scanning all ports. You already know what the lab simulators expose, and targeted scans are much faster.`,
      },
    ],
  }
;
