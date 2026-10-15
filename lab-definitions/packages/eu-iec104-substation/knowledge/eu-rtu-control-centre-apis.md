The RTU speaks IEC104 northbound and Modbus FC4 telemetry / FC6 commands southbound. It does not provide an operator REST API.

The native Control Centre exposes:

- GET `/api/health`
- GET `/api/state`
- GET `/api/journal?after=N`
- POST `/api/commands` with `{"point":"feeder-breaker-command","operation":"open"}`

The portal proxies `/api/operator/state`, `/api/operator/journal` and `/api/operator/commands`. Backend state adds `package_id` and `range_generation`.

Bad command input returns 400. A command already in flight returns 409. A link that is not started returns 503. A command-result status alone does not prove that the breaker moved. Check matching feedback and state evidence. The journal is a 500-entry ring. Use its `cache_epoch` and sequence bounds to check for missing entries. A truncated journal cannot prove a complete history.

The portal's read-only `/api/field/state` and `/api/field/audit?after=N` expose device state, solver output and device audit in the physical feeder section of `/substation`. They are not inputs to the CC.

Simulation control is executed through `POST /api/scenarios/iec104-telecontrol-boundary/steps/:stepIdx/execute`. The authored `local-trip`, `clear-fault` and `restore-device` steps use the EU recipe's breaker commands `inject_fault`, `clear_fault` and `reset`. The backend calls the breaker's management-network simulation endpoint (`POST /api/sim/fault` or `/api/sim/reset`), never US/RTAC control paths. Device audit records these actions as `lab`, retaining the real peer and zone. Reset Lab also resets the breaker.

The `cut-session` step freshly reads containd's sessions for package flow `cc-rtu` and deletes its exact original-direction tuple only when one complete match exists. No match, ambiguity, missing ports or an unavailable inventory means nothing is killed. Apply and verify a policy denying that conduit first: deleting conntrack sends no TCP reset and an allowed next packet can recreate the flow.
