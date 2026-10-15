A network map should explain roles and paths, not merely list reachable addresses. In this range, the station gateway, automatic controller, field equipment, process engine and historian are distinct actors. Following an IP address without its interface role can accidentally test a management path while claiming to have tested an operational conduit.

### Operational zones

The package uses these student-facing and operational networks:

| Firewall zone | Subnet | Nodes and purpose |
| --- | --- | --- |
| `wan` Enterprise | 10.10.10.0/24 | Kali at 10.10.10.50; enterprise probes and historian reads |
| `dmz` Vendor/Engineering | 10.20.20.0/24 | Vendor jump 10.20.20.10; engineering workstation 10.20.20.20 |
| `lan1` Station | 10.30.30.0/24 | RTU .20, PLC .30, GPS .50 |
| `lan2` Field | 10.40.40.0/24 | Breaker .20, RMU1 .21, RMU2 .22, OLTC .23 |
| `lan4` Control Centre | 10.60.60.0/24 | CC .20 |
| `lan5` Operations DMZ | 10.70.70.0/24 | Historian .40 |

The firewall is .2 in each routed zone. The similarly named Vendor/Engineering zone is not the Operations DMZ: their actors and permitted directions differ. The RTU has a station interface only. PLC and historian have no field or process interface, and do not forward packets.

### Support networks are not student conduits

Management is `10.99.99.0/24`, firewall zone `lan3`. It carries backend orchestration, portal proxies and simulation control. Important management endpoints on TCP/8080 are CC .21, breaker .22, solver .23, RMU1 .24, RMU2 .25, OLTC .26, GPS .27, PLC .30 and historian .40.

The internal `process_net`, `10.50.50.0/24`, is not a firewall zone. Solver .20 exchanges truth and measurements with breaker .30, RMU1 .31, RMU2 .32 and OLTC .33. Neither network is the student's simulated enterprise path into the plant. Successful backend access to a management endpoint cannot establish that the corresponding operational conduit is allowed.

Keep identities separate from service names. The breaker topology node is `breaker-1`, device id `feeder-breaker`, container `rangerdanger-feeder-breaker`. New device identities are `rmu-1`, `rmu-2`, `oltc-1` and `plc-1`; field-state keys use node ids. Do not join evidence by Docker service-name spelling alone.

### Trace a real path

For an IEC 104 operation, trace CC `10.60.60.20` to RTU `10.30.30.20:2404`, then RTU to the appropriate field TCP/502 endpoint. For automatic regulation, trace PLC `10.30.30.30` to OLTC `10.40.40.23:502`. For history, distinguish CC push to `10.70.70.40:8081` from enterprise GET on `10.70.70.40:8080`.

Use portal topology for orientation, package `manifest.json` for service roles/endpoints and Compose for interface assignments. Confirm the selected package and generation before taking captures. The [telecontrol architecture](#eu-telecontrol-architecture) explains which actor produced each observation; [policy comparison](#eu-policy-comparison) states which cross-zone paths should work.

### Limits

A multi-homed service is not automatically a router. The range disables forwarding on these nodes; management reachability still requires care in interpreting tests. Same-subnet RTU-to-PLC and RTU-to-GPS traffic is not enforced by the inter-zone firewall. The platform and range are separate Compose projects, not one monolithic stack. Do not import US node roles, old single-breaker assumptions or process-network enforcement claims into this package's map.

Implementation: package `compose.source.yml`, `compose.release.yml`, `manifest.json`, `topology.yml` and `process/profile.json`.
