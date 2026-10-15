A medium-voltage distribution feeder connects a primary substation to groups of customers. Switch positions determine which source can energise each section; impedances, load and transformer ratio determine the resulting voltages and currents. A switch diagram is therefore necessary but insufficient evidence of service. An open tie can separate two live sections, while a closed switch can connect two dead ones.

### The teaching circuit

The range uses a **CIGRE-derived teaching feeder**, reduced from European MV parameters with provenance and BSD-3 notices retained in the package process directory. It is not the CIGRE benchmark. The OpenDSS circuit is three-phase, 50 Hz, with a 110/20 kV primary transformer T1. Nominal connected real load is 2340.85 kW, distributed across three teaching load groups.

The normal arrangement is:

- T1 supplies the 20 kV bus through the [primary OLTC](#eu-primary-oltc).
- CB1, represented by `breaker-1`, supplies residential section S1.
- Normally closed RMU1 connects S1 toward commercial section S2.
- A second feeder head F2 supplies industrial section S3.
- Normally open RMU2 ties S3 to S2, leaving the ring electrically open.

Both RMUs call S2 their **section** side. RMU1's source side is S1; RMU2's source side is S3. These names describe fixed measurement locations, not the direction current must always flow. After back-feeding, current can reverse without the point names changing.

### Physics and protection are separate responsibilities

The solver reads every field device's truth, solves one consistent vector and posts measurements back to each device. The configured solve interval is 1000 ms. Devices reject measurements calculated from an obsolete truth revision; a trip or other revision change can trigger one immediate correction solve within the tick. This prevents the displayed post-trip circuit from being fabricated by simply forcing current to zero.

Faults belong to the process: `fault_s1` and `fault_s2` introduce bolted three-phase faults at the corresponding buses. The [breaker](#eu-feeder-breaker-protection) trips when measurements show it carries fault current. F2 is a process-owned, latched head trip, not a separate controllable field device. An RMU indicates fault passage but is not a fault-interrupting breaker.

### How to observe the feeder

In the portal's `/substation` view, keep the CC-observed one-line separate from the physical device/process section. Compare section voltages, positions and quality before and after each authored switching or fault step. The read-only portal export `GET /api/field/state` contains a `devices` map plus `process`; the latter includes `buses`, `fault_section`, `f2_tripped`, `solver_epoch` and `solve_seq`.

On the CC side, S3 voltage is `rmu-2-source-kv`, IOA 2203. F2 has no IEC 104 position point. An unobserved F2 symbol is not an open indication. Use [reference cases](#eu-process-reference-cases) to distinguish expected solved behaviour from a current observation, and [switching orders](#eu-rmu-switching-orders) before attempting restoration.

### Limits

This is a static power-flow and simplified fault-path teaching model, not electromagnetic transient simulation, relay coordination, arc-flash analysis or an operational switching authority. The solver can solve a closed ring honestly, but device interlocks normally prevent live-live closing. Loss of truth retains the last good process state with `truth_stale`; it does not assume a switch position. Delivered measurements expire after three seconds, so retained numbers without valid quality cannot establish that a section is safe.

Implementation: package `process/feeder.dss`, `process/profile.json`; `services/opendss-sim/eu_circuit.py` and `eu_truth_loop.py`.
