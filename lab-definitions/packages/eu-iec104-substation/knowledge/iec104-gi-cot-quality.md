Station General Interrogation uses QOI 20: activation (COT 6), activation confirmation (7), monitored values (20), then activation termination (10). Spontaneous reports use COT 3; command feedback uses COT 11. A negative confirmation is a separate indication, not a different COT number.

Quality flags describe the observation. GOOD means good quality; IV means invalid, NT not topical, SB substituted, BL blocked, and OV overflow. This vocabulary does not imply the simulator generates every flag.

After three seconds of stale Modbus data, the RTU reports IV+NT; the CC maps NT to `device_unreachable`. Measurement expiry marks floating-point measurements IV; the CC maps IV alone to `measurement_invalid`. CC link loss preserves last values and times, marks them IV+NT with `link_lost`, and does not refresh from management HTTP. A point never received is null, not zero. Fresh IEC104 reports or GI are needed for recovery.

GI values and floating-point measurements are untimed. Timed status carries RTU observation time, not device-provided sequence-of-events time. [Timestamp validity](#eu-station-clock) is separate from point-quality IV.

Use [monitor](#tool-iec104cmd) to see point objects; a one-shot GI JSON result is not a point listing.

Sources: `services/iec104/README.md` lines 79–94, 148–154; `src/rtu.c` lines 160–168; `src/common.c` lines 58–80.
