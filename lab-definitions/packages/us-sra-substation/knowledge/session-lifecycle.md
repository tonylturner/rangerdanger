# Session lifecycle

A workshop reset reapplies Weak Baseline, resets the retained feeder devices
and ends broker sessions. It does not reset identities, TOTP or recordings.
Treat a reset as a new observation baseline and reconnect explicitly.

A range start recreates the identity provider, session database and overlay
state. Enrollment tokens and recordings are package-scoped host directories
whose contents are cleared by the new containers at first boot. Export any
recording needed for grading before starting or switching the range; its
database history is not carried into the next range generation.

A plain container restart does not wipe or reseed identities or recordings.
Container health is not evidence that an overlay tunnel is online: the OT
host must be able to start before the firewall policy is applied. Check the
actual brokered connection separately when collecting access evidence.
