Protective relays detect abnormal electrical conditions and trip circuit breakers to isolate faulted sections of the power system. Without them, a short circuit on a distribution feeder would produce sustained fault current that destroys transformers, burns conductors, and endangers workers and the public.

### How Relays Work

Modern microprocessor-based relays (SEL-751, SEL-351, GE Multilin 750) pack multiple protection functions into a single device. A typical feeder relay implements:

- **Overcurrent protection** (50/51 elements)
- **Reclosing logic**
- **Undervoltage** (27) and **overvoltage** (59)
- **Frequency elements** (81)

The relay continuously samples current and voltage waveforms, runs protection algorithms, and makes trip/close decisions in milliseconds. When a fault is detected, the relay asserts its trip output, which energizes the trip coil on the circuit breaker to open it.

### Why Relay Compromise is Dangerous

Unauthorized access to a protective relay is one of the highest-impact attacks in a substation. An attacker who can talk to the relay over Modbus or DNP3 could:

- **Disable protection elements**, preventing the relay from tripping on faults
- **Change pickup settings**, so faults go undetected
- **Directly command the breaker** to open (causing an outage) or close onto a faulted line (causing arc flash or equipment destruction)

Disabling overcurrent protection and then creating a fault condition means sustained fault current with no automatic interruption. That causes real physical damage.

### Lab Implementation

The relay simulator (`relay-sim`) runs at **10.40.40.20** on `field_net`. It models a feeder breaker with trip/close control, lockout state, and fault injection. It exposes state over three interfaces:

| Protocol | Port | Details |
|----------|------|---------|
| HTTP REST | 8080 | `GET /api/state`, `POST /api/command` |
| Modbus TCP | 502 | FC01/FC03/FC05/FC06 |
| DNP3 TCP | 20000 | Outstation address **1** |

Under the weak firewall policy, any host on any zone can send Modbus write commands to the relay. Under the hardened policy, only the RTAC at 10.30.30.20 can.