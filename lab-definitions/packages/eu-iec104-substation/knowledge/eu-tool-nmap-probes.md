`nmap` can test a narrowly defined network boundary, but it cannot prove that a protocol operation succeeded or that a controller is healthy. Kali, engineering and vendor images explicitly install it. These recipes run from Kali at `10.10.10.50`, so the result describes enterprise-originated traffic.

### Probe the known listener, not the whole subnet

```sh
mkdir -p /home/kali/workspace
nmap -n -Pn -sT -p 2404 --max-retries 1 --host-timeout 15s \
  -oN /home/kali/workspace/rtu-probe.txt 10.30.30.20
```

`-n` avoids DNS, `-Pn` skips a host-discovery prerequisite and `-sT` uses ordinary TCP connect. The known destination is the RTU; there is no need to sweep every address or all 65,535 ports. The reduced retry budget keeps the exercise bounded but also limits how much can be concluded from a timeout.

Under weak policy the listener should be reachable. Under improved policy a new Kali-to-RTU connection should be denied. Save each run with a different filename and record the active policy and attempt times.

### Test field and archive boundaries separately

```sh
nmap -n -Pn -sT -p 502 --max-retries 1 --host-timeout 15s \
  -oN /home/kali/workspace/oltc-probe.txt 10.40.40.23
nmap -n -Pn -sT -p 8080,8081 --max-retries 1 --host-timeout 15s \
  -oN /home/kali/workspace/historian-probe.txt 10.70.70.40
```

The improved design denies enterprise access to OLTC Modbus and historian ingest, while allowing historian reads on 8080. A successful archive-read TCP connection does not authorize writes to ingest or switching through the historian.

The expected new-connection matrix from Kali is:

| Destination | Weak | Improved |
|---|---|---|
| RTU TCP/2404 | Allowed | Denied |
| OLTC TCP/502 | Denied | Denied |
| Historian TCP/8080 | Allowed | Allowed |
| Historian TCP/8081 | Denied | Denied |

“Denied” describes policy intent, not a guarantee of the scanner's exact display wording. In particular, weak policy's broad Modbus permission is station-to-field, not enterprise-to-field. The lab must not invent a successful Kali Modbus baseline to make the comparison look more dramatic.

These are different assertions. Do not report “historian isolated” if only 8081 was tested, or “field protected” after probing a single service without identifying the tested scope.

### Understand the result vocabulary

- **Open:** a TCP connection was accepted. It says nothing about valid IEC 104 STARTDT/GI, Modbus register semantics or HTTP authorization.
- **Closed:** the probe reached something that actively refused the connection. That is not the same as a firewall silently dropping it.
- **Filtered / timeout:** no decisive response arrived within the scan's budget. Correlate with policy and firewall evidence; service failure or routing can produce similar symptoms.

Keep filtered results in the output; `--open` would hide precisely the negative evidence being collected. Avoid `-sV`, `-A`, NSE scripts and aggressive discovery here. They send additional application probes that the boundary question does not require.

### Pair reachability with real operations

After a weak-policy RTU probe, use a bounded [GI check](#tool-iec104cmd). After an allowed historian probe, perform the GET in [curl exports](#eu-tool-curl-exports). To show improved policy preserved operations, inspect fresh CC telemetry and the PLC/OLTC loop through the lab workflow; Kali cannot stand in for those authorized sources.

Also distinguish a new scan connection from a monitor established before the policy change. Conntrack may preserve the latter. The [session article](#eu-established-sessions) explains why their outcomes can legitimately differ.

Finally, this is a simulated, authorized range. These command budgets and known addresses are not permission to scan a real substation. Production discovery needs an asset-owner-approved method, timing and stop condition suited to the actual equipment.
