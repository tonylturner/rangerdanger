The seeded PLC/OLTC loop and its evidence are optional deeper study, not a mandatory long-running test in Lab 2.4. Keep recorded seed sha256, loaded ST and slave mappings; do not recompile to force an expected voltage. OLTC FC4 IR0…14 maps to PLC %IW100…; absolute adjacent target requests use HR1 from %QW100. PLC status is FC3 HR1024…1029, with changing heartbeat, not a field FC4 register block.

### Delay, motion and operating points
The setpoint is 20.4 kV with a 20.196–20.604 kV band (±1%). Valid energized remote/auto unblocked input must remain out of band for **30 s** before a demand; motion takes **2 s**. The OLTC sits on the 110 kV winding: ratio = 1 − 0.0125 × operator tap. Positive tap raises MV voltage; ±8 ratio steps produce an asymmetric +11.1% / −9.1% MV range. During motion completed tap stays at the old value with transient true, not the target immediately. A PLC request is 100 + completed ± 1, held until completion/inhibition, then zero; a zero-before-request handshake prevents replay on takeover.

With the daily profile disabled and initial tap zero, nominal settles at +2 (20.429 kV); load 3.0 also settles at +2 (20.203 kV). Predict raises from zero and retain the delay/request/completion trend. A settled +2 baseline need not require another step at load 3.0. At load 1.0 and source 0.90 p.u., the reference reaches **+8** with residual low voltage: saturation, not a dead PLC. Source 1.10 settles near **−6**, about 20.382 kV, not necessarily −8. These reference cases disable the daily load/source profile. The enabled Europe/Berlin profile multiplies the instructor load/source controls; reset does not stop it or re-enable it if disabled. Re-enable it after deterministic checks. Record actual values and never tune feeder parameters to match the answer. A final in-band sample cannot prove timing or adjacent motion.

### Authority and handover
Remote plus manual permits CC regulating-step commands; local/remote is separate from auto/manual. C_SC 3302 selects manual/auto, with matching feedback 1303; C_RC 3301 handles raise/lower. Directional limit behavior means one extra lower at −8 is refused while an inward raise is permitted. Restore nominal source and automatic regulation after investigation. OLTC protection blocks new motion at 866 A solved current, in addition to the manual block; `oltc-1-overcurrent-block` is IOA 1307. T1 P/Q/PF are IOAs 2303–2305 and feeder P/Q/PF are 2003–2005. IOA 2403 reports the configured 30 s delay, not the internal countdown. Vendor tap misuse under weak has a distinct Modbus peer; hardened source pinning closes it without breaking PLC→OLTC or allowed CC operation. No new attack recipe is introduced here.

### Historian uncertainty and the three clocks
A CC outage can leave retained nonzero values marked IV+NT/link_lost or a feed gap. Neither is fresh zero. Archive segments carry cache_epoch; samples carry snapshot_seq, CC received time and ingested time. A gap is not a physical outage measurement. Keep policy/session intervention, stale interval, reconnect and recovery GI temporally ordered. The [historian](#eu-historian) reference covers queries and uncertainty.

Station-time offset changes RTU observation provenance, not solved physics or CC receipt time. Compare a timestamped spontaneous point before/after a known positive offset (over 60 s), then a restored offset below 5 s. GI is untimed and cannot establish this clock test. Reset GPS offset, restore source/load and wait for fresh GOOD telemetry and the normal loop before handover. Export audit, trend, quality, source time, receipt time and archive ingestion time separately.

### Reflection questions

These optional questions preserve the operational investigation; they are not additional graded workshop steps. A denied path can be established by strict active probes without mandatory logged-deny evidence.

#### oltc-load-direction

Which direction should the PLC request after the load depresses bus voltage?

Expected reasoning: raise.

#### oltc-motion-value

What should completed-tap telemetry show during mechanical motion?

Expected reasoning: old value transient.

#### oltc-saturation

How do you describe +8 with fresh input and voltage still below band?

Expected reasoning: limit with residual error.

#### oltc-high-source

Why can the PLC stop at −6 with source 1.10?

Expected reasoning: settle in band before minus8.

#### oltc-manual-authority

What authority and mode permit a remote CC regulating-step command?

Expected reasoning: remote and manual.

#### oltc-vendor-weak

What failed when a vendor-origin manual tap operation reached the actuator?

Expected reasoning: source boundary missing.

#### oltc-outage-history

What may the historian honestly show during loss of CC visibility?

Expected reasoning: stale or gap.

#### oltc-clock-order

Which evidence preserves observation order across a station clock correction?

Expected reasoning: receipt and sequence.
