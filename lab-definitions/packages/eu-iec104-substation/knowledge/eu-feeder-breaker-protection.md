This lab implements one feeder breaker, initially closed and remote-enabled. It does not implement RMUs, OLTCs, reclosers, voltage regulators or capacitor banks.

A fault locally opens the breaker and latches protection trip and lockout. Clearing the fault does not clear those latches. Instructor reset restores initial truth. The breaker rejects a close command while lockout is set. A permitted command still produces feedback if the breaker is already in the requested position.

Local protection and the solver continue to work when the Control Centre loses its connection. During a [telecontrol outage](#eu-established-sessions), the operator can retain stale observations while the device and solver advance.

The process profile is 20 kV, 50 Hz, 1000 kW and power factor 1. It is not a full CIGRE, earthing, protection-selectivity or transient model.
