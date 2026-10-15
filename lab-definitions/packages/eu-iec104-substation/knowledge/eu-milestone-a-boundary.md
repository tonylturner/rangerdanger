Weak policy permits TCP/2404 from wan, dmz and lan4 into lan1 and TCP/502 from lan1 into lan2. Improved policy source-pins Control Centre `10.60.60.20` to RTU `10.30.30.20:2404` and that RTU to breaker `10.40.40.20:502`.

The allowed CC still opens AND closes. There is no ASDU-, CA- or IOA-aware command filtering. The package declares `ics_rules: forbidden`; packet decoding in a capture does not add firewall semantics. Milestone B belongs to separate containd work, not this slice.

[SBO](#iec104-sbo-commands) constrains command sequencing, not caller authorization. [Established sessions](#eu-established-sessions) also explain why applying improved policy need not immediately interrupt a preexisting flow.

Sources: EU package.yml lines 7–12; EU scenario lines 12–15, 80–94.
