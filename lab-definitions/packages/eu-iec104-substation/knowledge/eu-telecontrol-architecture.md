The Control Centre (CC) at `10.60.60.20` is an IEC104 client. It connects to the RTU server at `10.30.30.20:2404`. The RTU reaches the feeder breaker at `10.40.40.20:502` using Modbus TCP. These are two separate network boundaries, not a direct CC-to-breaker connection.

Operator state comes only through IEC104. On [/substation](/substation), the portal also shows a clearly separate physical feeder section: direct device state, OpenDSS output, an audit of accepted and rejected actions, and a field-versus-control-centre comparison. These readings never refresh the CC cache.

The simulation control plane is separate from telecontrol. The breaker serves its `sim` endpoint on `mgmt_net` for backend lab-step actions and Reset Lab. The physics exchange (device state to OpenDSS, solved measurements back to the device) uses `process_net`. The CC shares the management network with these services, so isolation is behavioral, not a network enforcement claim: the workshop checks that the CC reads only IEC104.

Modbus southbound is a deliberate simplification. This lab does not emulate IEC 61850, IEC101 or IEC103.

Read [quality and stale observations](#iec104-gi-cot-quality) and [station time](#eu-station-clock) before treating a cached value as current physical state.
