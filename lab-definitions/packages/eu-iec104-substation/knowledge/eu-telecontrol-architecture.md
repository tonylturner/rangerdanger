The Control Centre (CC) at `10.60.60.20` is an IEC104 client. It connects to the RTU server at `10.30.30.20:2404`. The RTU reaches the feeder breaker at `10.40.40.20:502` using Modbus TCP. These are two separate network boundaries, not a direct CC-to-breaker connection.

Operator state comes through IEC104. Instructor and process APIs expose separate physical truth. The Control Centre shares the management network with those services. The network does not prevent it from reading their HTTP endpoints. The workshop checks that it learns process state through IEC104 instead.

Modbus southbound is a deliberate simplification. This lab does not emulate IEC 61850, IEC101 or IEC103. Its process plane is separate from the operator's telecontrol path.

Read [quality and stale observations](#iec104-gi-cot-quality) and [station time](#eu-station-clock) before treating a cached value as current truth.
