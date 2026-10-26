# Secure remote access workshop — implementation specification

Status: proposed lab track; runtime integration is not implemented by this change.
The current Compose stack does not install Keycloak, Guacamole, or OpenZiti.
Do not advertise this track as classroom-ready until the acceptance gates below pass.

## Scope and learning objectives

A standalone four-hour workshop in RangerDanger, alongside the existing segmentation
track. Reuse containd, the existing vendor workstation, OT simulation, process view,
packet capture, and exercise runner. Do not require students to complete the existing
segmentation labs first. Do not fork the DNP3 or IEC 104 scenario implementation.
Remote access terminates at a maintenance workstation; field equipment is not the
normal remote-access entry point.

Students must distinguish human authentication, device/workload identity, network
reachability, application authorization, target login, and process authorization.
They must demonstrate allowed and denied access, identify evidence, and terminate
vendor access without interrupting the running process.

## Four-hour teaching plan

| Minutes | Format | Activity and result |
|---|---|---|
| 0–25 | Slides | Remote maintenance task, OT boundaries, identity versus connectivity, VPN/gateway/overlay comparison |
| 25–50 | Lab | Connect the preconfigured VPN; discover excessive reach; tighten containd rules; preserve the approved maintenance path |
| 50–70 | Slides/demo | Human identity, MFA, gateway authorization, separate target credentials, recording |
| 70–105 | Lab | Keycloak login and MFA; Guacamole authorized session; denied second connection; inspect session evidence |
| 105–115 | Break | |
| 115–140 | Slides/demo | Ziti enrollment, identities, dial/bind policies, underlay versus overlay, encrypted traffic visibility |
| 140–185 | Lab | Reach an authorized SSH service through OpenZiti; deny another service and the direct path; change service policy |
| 185–220 | Lab | Revoke human and overlay access separately; test new connections and existing sessions; terminate access at the correct layer |
| 220–240 | Debrief | Submit evidence matrix; explain residual reach and confirm continued local operation |

Use one hands-on VPN protocol per delivery. Both OpenVPN and WireGuard are provided
by containd; briefly demonstrate the alternative. Keycloak MFA must not be claimed
for either VPN until its actual integration is implemented and verified. WireGuard
peer keys identify a peer, not automatically the person at the keyboard.

## Maintenance mission

A named external maintainer needs an interactive session to inspect an engineering
workstation and diagnose a feeder issue. The approved task is read-only diagnosis.
Use an existing simulator state/read endpoint for the final operational check; do
not open a breaker as part of the normal maintenance task. An optional instructor
controlled consequence demonstration can show that a successful remote login does
not authorize every downstream process action.

Three independent access patterns support comparison. Do not require VPN,
Guacamole, and OpenZiti in series for every exercise.

1. VPN client -> containd VPN -> firewall policy -> maintenance SSH endpoint.
2. Browser -> Guacamole -> guacd -> maintenance SSH/RDP endpoint.
   Browser login redirects to Keycloak; target login uses separate target credentials.
3. Vendor endpoint with Ziti identity -> edge router -> service-hosting component
   -> maintenance SSH endpoint. Keycloak can authenticate the user to the overlay
   through an external JWT signer where supported by the selected client version.
   Certificate enrollment alone is a valid first exercise; do not imply human SSO
   when only a workload certificate was used.

For browser-only delivery use a dedicated vendor-client container with terminal
access through the existing portal. It must be on the external side of the boundary,
not the existing vendor DMZ jump host. A tunneler shares that client network namespace
(or runs in that container). The client must not also join the target network.
Keep its identity separate from the identity of the service-hosting connector.

## Deployment work

Add an opt-in `docker-compose.sra.yml` override. Starting the existing workshop
without the override must retain its existing services, ports, and policies.
Document source-build and release-image invocation separately.

| Component | Required configuration |
|---|---|
| containd | Existing image; TUN device mapping; publish only the selected lab VPN port on loopback; seeded VPN clients and explicit forward/return rules |
| Keycloak | Version-pinned image; imported `rangerdanger-sra` realm; named vendor and employee accounts; required TOTP enrollment; separate OIDC clients for Guacamole and supported Ziti client |
| PostgreSQL | Version-pinned image; separate database users/databases for Keycloak and Guacamole; healthcheck and persistent storage |
| Guacamole | Matched Guacamole/guacd versions; database schema initialized once; OIDC extension; database-backed connection permissions |
| guacd | No host-published port; recording volume shared with the recording playback mechanism; only the needed target routes |
| Ziti controller | Persistent PKI/database; explicit advertised name; authentication and service policies; admin API inaccessible from student data plane |
| Ziti edge router | Explicit reachable advertised address; router enrollment; constrained underlay routes through containd |
| Ziti host | Dedicated bind identity; only maintenance SSH service hosted; no broad subnet forwarding |
| Vendor client | Distinct identity; SSH and bounded probe tools; selectable VPN and overlay paths; no embedded private keys in images |
| Maintenance target | Reuse an existing SSH-capable host if its topology permits the isolation tests; otherwise add a dedicated target with equivalent OT read tools |

Use distinct persistent volumes and generated secrets. Lab accounts may use documented
classroom credentials, but enrollment tokens, private keys and OIDC client secrets
must be generated locally and excluded from git. Verify image architectures before
adding them to offline bundles. Stage every optional image for amd64/arm64 where
supported; fail bundle staging if a required image cannot be loaded.

## Network and identity design

Allocate new addresses only after checking existing topology and conflicts. Reuse
existing zone names where appropriate, but do not overload a segmentation exercise's
weak/hardened policies with SRA transitions. Provide dedicated SRA policy snapshots
and give the UI a clear active-track label.

Ordinary workloads remain single-homed. All intended cross-zone underlay traffic
must route through containd with explicit return routes. Docker's bridge gateway
is not automatically containd. Same-bridge traffic cannot be used as proof of firewall
enforcement. A Ziti connector's target leg must have only the intended service reach.

Keycloak and database services use a service/control network, not a broad management
leg on every student workload. Guacamole must not gain a direct target path that is
presented to students as overlay-protected. Existing portal terminals, Docker exec,
and proxied target UIs are out-of-band training tools: label them as such and exclude
them from access-control proof. Do not assert production isolation from a host user
who controls Docker.

Use stable browser-reachable OIDC issuer URLs. The issuer hostname must also resolve
inside containers; do not mix `localhost` and container-only issuers. Provide local
TLS trust bootstrap and accurate system clocks. Validate redirects, token audience,
claim-to-identity mapping, and logout behavior. The identity provider remains reachable
during onboarding; access to the identity provider must not depend on an overlay
identity the user is still trying to obtain.

Containd sees the overlay underlay flow. It cannot inspect Modbus/DNP3 payloads inside
an encrypted Ziti connection. Apply any desired OT DPI on the decrypted connector-to-
target leg when that leg actually crosses the firewall. State this visibility boundary
in the topology and slides.

## Exercise integration

Author four scenario YAMLs under `lab-definitions/scenarios/` only when their
runtime dependencies exist. Suggested IDs: `sra-vpn-boundaries`, `sra-brokered-sessions`,
`sra-overlay-services`, `sra-revocation-evidence`. Add an SRA track filter to the
exercise inventory so the four-hour track is separate from segmentation ordering.

Update topology node inventory and `frontend/lib/exercise-nodes.ts` for new terminal
sources. Keep the existing bonus vendor-compromise scenario unchanged. Route SRA
reset through a distinct operation: restore named accounts, permissions, VPN peers,
Ziti policies/identities, target state and clean evidence. End sessions before resetting;
do not delete/recreate identity-provider signing keys on each student reset.

Implement dedicated backend validators. The current generic validator checks process
state and must never produce an SRA pass merely because the feeder is healthy.
Missing optional services should return a clear unavailable/prerequisite result.
Check both listener health and reachability: connection refusal is not a successful
maintenance login, and a timeout alone is not proof of policy enforcement.

## Student evidence matrix

For each row record source, human identity, device/workload identity, destination,
protocol, expected result, observed result, UTC timestamp, enforcement point, and
matching event/session identifier. Record errors as errors, not denials.

| Test | Expected observation |
|---|---|
| VPN approved destination | Successful authenticated maintenance session |
| VPN unrelated OT/field destination | Denied by intended policy; matching firewall evidence |
| Guacamole approved connection | Named human login, MFA, authorized target session and recording |
| Guacamole unauthorized connection | Permission absent or explicitly denied; no launch |
| Overlay approved service | Successful SSH through dial/bind identities and service policy |
| Overlay unauthorized service | Authorization denied with controller/client evidence |
| Direct target connection | No bypass path from the vendor client |
| Disable human account | Test fresh browser login, existing SSO token, and existing remote session separately |
| Revoke overlay permission/identity | Test new dial and established connection separately; record actual version behavior |
| Terminate active access | Explicitly close gateway/target/overlay sessions as required and prove they ended |
| Operational continuity | Existing OT communications and local operator view still work |

Do not prescribe that removing a policy or disabling an account immediately kills
all established sessions. Establish the selected versions' behavior experimentally.
A successful access-withdrawal outcome requires both failed new access and ended
active access, with local operation preserved.

## Acceptance gates before classroom release

- Base dev/release Compose configurations still validate; optional SRA merge validates.
- Cold start, warm restart, clean reset and second reset all succeed without manual edits.
- Click through the complete four-hour track on amd64 and Apple Silicon delivery targets.
- Test TLS trust, MFA enrollment, incorrect MFA, unauthorized connection, and named-user recording playback.
- Run every allowed/denied matrix probe from the actual VPN/gateway/overlay source namespace.
- Capture matching firewall, IdP, gateway and Ziti evidence; verify the stated attribution limits.
- Test active-session revocation separately from new-session denial.
- Verify OT polling and process state throughout reset and revocation.
- Exercise runner cannot report PASS with SRA services stopped or unprovisioned.
- Existing segmentation smoke gates still pass after disabling the SRA track.
- Verify a fully offline install with optional images, seeded configuration and local certificates.

## References

- [RangerDanger authoring conventions](../lab-authoring.md)
- [RangerDanger security model](../../SECURITY.md)
- [Containd services](https://github.com/tonylturner/containd/blob/main/docs/mkdocs/services.md)
- [Guacamole Docker deployment](https://guacamole.apache.org/doc/gug/guacamole-docker.html)
- [Guacamole OIDC](https://guacamole.apache.org/doc/gug/openid-auth.html)
- [Keycloak containers](https://www.keycloak.org/server/containers)
- [OpenZiti](https://github.com/openziti/ziti)
