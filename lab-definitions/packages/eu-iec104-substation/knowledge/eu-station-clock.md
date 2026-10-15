The RTU polls gps-sim over HTTP every 1000 ms. It accepts a locked source and treats it as stale after 5000 ms. Reported time advances using monotonic elapsed time. Without a base, host time is used unsynchronized.

Timed-status CP56Time2a IV follows synchronization. Timestamp invalidity (`source_time_invalid`) is separate from point-quality IV. An uncertain timestamp does not mean the point value is invalid. GI and floating-point reports are untimed. Timed statuses record RTU observations, not device sequence-of-events (SOE) times.

The implemented path is HTTP-fed station time. gps-sim is not an NTP server. This path does not use NTP, PTP, IRIG-B or Type-103 clock synchronization.

See [GI and quality](#iec104-gi-cot-quality).
