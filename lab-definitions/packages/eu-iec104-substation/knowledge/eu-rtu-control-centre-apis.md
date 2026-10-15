The RTU speaks IEC104 northbound and Modbus FC4 telemetry / FC6 commands southbound. It does not provide an operator REST API.

The native Control Centre exposes:

- GET `/api/health`
- GET `/api/state`
- GET `/api/journal?after=N`
- POST `/api/commands` with `{"point":"feeder-breaker-command","operation":"open"}`

The portal proxies `/api/operator/state`, `/api/operator/journal` and `/api/operator/commands`. Backend state adds `package_id` and `range_generation`.

Bad command input returns 400. A command already in flight returns 409. A link that is not started returns 503. A command-result status alone does not prove that the breaker moved. Check matching feedback and state evidence. The journal is a 500-entry ring. Use its `cache_epoch` and sequence bounds to check for missing entries. A truncated journal cannot prove a complete history.

Instructor `/api/instructor/state`, `/api/instructor/audit`, `/api/instructor/fault` and `/api/instructor/reset` belong to a separate plane. Do not use US direct-device `/api/command` recipes here. See [architecture](#eu-telecontrol-architecture).
