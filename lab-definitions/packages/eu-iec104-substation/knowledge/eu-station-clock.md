The RTU polls gps-sim over HTTP every 1000 ms, accepts a locked source, and treats it as stale after 5000 ms. Reported time advances using monotonic elapsed time. Without a base, host time is used unsynchronized.

Timed-status CP56Time2a IV follows synchronization. Timestamp invalidity (`source_time_invalid`) is separate from point-quality IV: an uncertain timestamp and an invalid value are different evidence. GI and floating-point reports are untimed; timed statuses are RTU observations, not device SOE.

The implemented path is HTTP-fed station time. gps-sim is not an NTP server; this path is not NTP, PTP, IRIG-B or Type-103 clock synchronization. The scenario has no GPS exercise.

See [GI and quality](#iec104-gi-cot-quality).

Sources: package `protocols/points.json` lines 14–18; `services/iec104/src/stationclock.c` lines 243–263, 309–322; `src/rtu.c` lines 149–155; `services/README.md` lines 37–43.
