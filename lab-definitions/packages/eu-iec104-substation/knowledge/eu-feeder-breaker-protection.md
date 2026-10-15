This slice implements one feeder breaker, initially closed and remote-enabled. It does not implement RMUs, OLTCs, reclosers, voltage regulators or capacitor banks.

A fault locally opens the breaker and latches protection trip and lockout. Clearing the fault does not clear those latches. Instructor reset restores initial truth. Closing under lockout rejects; a permitted no-op still produces feedback.

Local protection and solver truth are independent of Control Centre reachability. During a [telecontrol outage](#eu-established-sessions), the operator can retain stale observations while the device and solver advance.

The process profile is 20 kV, 50 Hz, 1000 kW and power factor 1. It is not a full CIGRE, earthing, protection-selectivity or transient model.

Sources: `services/devices/feederbreaker/model.go` lines 149–159, 206–279; `services/devices/httpapi/server.go` lines 42–56; package `process/profile.json` lines 3–20.
