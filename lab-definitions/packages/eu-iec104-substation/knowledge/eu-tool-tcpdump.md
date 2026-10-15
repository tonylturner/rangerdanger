A packet capture is a record from one observation point, not an omniscient view of the range. `tcpdump` is explicitly installed in Kali, engineering and vendor images. The recipes below use Kali's terminal and save output under `/home/kali/workspace`.

### Capture one conduit attempt

Start this in one Kali terminal before running a short GI/monitor in another:

```sh
mkdir -p /home/kali/workspace
tcpdump -ni any -s 0 -U \
  -w /home/kali/workspace/kali-rtu.pcap \
  'host 10.30.30.20 and tcp port 2404'
```

Stop it with Ctrl-C after the bounded exercise. `-n` avoids name resolution, `-i any` avoids guessing an interface name, `-s 0` retains full packets and `-U` flushes packet output promptly. This captures Kali's side of the RTU attempt. It will not reveal an unrelated CC session just because that session also uses port 2404.

Use a second narrowly scoped capture when testing historian reads:

```sh
tcpdump -ni any -s 0 \
  -w /home/kali/workspace/kali-historian.pcap \
  'host 10.70.70.40 and tcp port 8080'
```

Pair it with the direct historian GET in [curl exports](#eu-tool-curl-exports). An archive-read capture and an ingest capture answer different questions; do not combine port 8080 and 8081 under a vague “historian works” claim.

### Read and preserve the result

```sh
tcpdump -nn -tttt -r /home/kali/workspace/kali-rtu.pcap
```

This gives a transport-level timeline. Use [tshark](#tool-iec104-tshark) for IEC 104 fields, quality and command sequencing. Preserve the raw capture even when exporting a convenient table; a table may omit multiple ASDUs or points sharing a packet.

Record the node, capture start/stop times, filter, active policy and operation attempted. Keep weak/improved captures under distinct names. Capture before opening the test connection so the handshake and original-direction tuple are available.

### Interpret silence conservatively

SYN retransmissions without replies support a failed connection attempt from this observation point. They do not by themselves distinguish a firewall drop, routing failure or unavailable service. Pair them with the active policy, matching firewall evidence and a healthy authorized path.

A successful handshake proves transport reachability, not GI completion or permission to switch. A capture containing no application data cannot demonstrate that an IEC 104 command was rejected by the RTU. Conversely, a protocol rejection may prove that the packet reached the application rather than that the firewall blocked it.

Watch the capture summary for dropped packets. A missing termination in an incomplete capture is not proof that the device never sent it. TCP retransmissions must not be counted as independent device operations.

### Respect the observation boundary

Engineering and vendor have different allowed conduits from Kali. A capture taken there describes that host's traffic, not an equivalent enterprise test. Management-facing portal traffic is also different from a direct station or field connection.

For CC-to-RTU or PLC-to-OLTC traffic, use the lab's designated firewall-side capture workflow rather than assuming an endpoint can promiscuously see other Docker bridges. Do not add interfaces, change routes or attach a container to another network for visibility. The [zone map](#eu-zone-conduit-design) and [evidence lifecycle](#eu-evidence-lifecycle) explain why preserving the original path is part of the proof.
