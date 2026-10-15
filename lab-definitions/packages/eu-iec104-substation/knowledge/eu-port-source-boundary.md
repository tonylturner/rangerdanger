The weak policy permits TCP/2404 from wan, dmz and lan4 into lan1. It also permits TCP/502 from lan1 into lan2. The improved policy allows only Control Centre `10.60.60.20` to RTU `10.30.30.20:2404` and that RTU to breaker `10.40.40.20:502`.

These are port- and source-based rules. The allowed Control Centre still opens and closes the breaker. Command-aware IEC 104 filtering is not part of this lab. The firewall does not filter commands by ASDU type, CA or IOA. Decoding packets in a capture does not change what the firewall enforces.

[SBO](#iec104-sbo-commands) checks command sequencing, not caller authorization. Read [established sessions](#eu-established-sessions) to see why applying the improved policy may leave an existing connection open.
