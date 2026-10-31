`tshark` is the command-line version of Wireshark. In the lab, you use it to analyze network traffic and verify what protocols are flowing between zones. It is available on the Kali box.

### Protocol Volume Summary (io,stat)

To see a summary of protocol traffic over time:

```bash
tshark -r capture.pcap -q -z io,stat,10,modbus,dnp3
```

This gives you a table showing packet counts for Modbus and DNP3 traffic in 10-second intervals. Useful for seeing traffic patterns and spotting when attack traffic starts.

### Host Pair Summary (conv,ip)

To see which IP addresses are talking to each other:

```bash
tshark -r capture.pcap -q -z conv,ip
```

This produces a table of all IP conversation pairs with packet counts and byte totals. You can quickly identify which hosts are generating the most traffic and spot unexpected communication paths (like enterprise hosts talking directly to field devices).

### TCP Connection Detail (conv,tcp)

To see individual TCP connections:

```bash
tshark -r capture.pcap -q -z conv,tcp
```

This shows source/destination IP:port pairs for every TCP session. Useful for confirming which specific services are being accessed (port 502 = Modbus, port 20000 = DNP3, port 8080 = HTTP REST).

### Display Filters

tshark supports Wireshark display filter syntax for narrowing results:

```bash
# Only Modbus traffic
tshark -r capture.pcap -Y "modbus"

# Modbus writes only (FC05 or FC06)
tshark -r capture.pcap -Y "modbus.func_code == 5 || modbus.func_code == 6"

# DNP3 traffic to the relay
tshark -r capture.pcap -Y "dnp3 && ip.dst == 10.40.40.20"

# All traffic from the Kali box
tshark -r capture.pcap -Y "ip.src == 10.10.10.50"
```

### Live Capture

To capture live traffic on an interface:

```bash
tshark -i eth0 -w capture.pcap
```

Add `-f "port 502"` as a BPF capture filter to limit what gets written to the file. Use `-c 100` to stop after 100 packets.

> The three views (io,stat, conv,ip, conv,tcp) give you a quick top-down picture of network activity without scrolling through individual packets. Start there before diving into packet-level analysis.