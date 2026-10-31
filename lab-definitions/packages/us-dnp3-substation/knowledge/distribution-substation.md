A distribution substation converts high-voltage bulk power (69 kV to 345 kV) down to the distribution-level voltages (4 kV to 34.5 kV) that actually serve homes and businesses. Transmission lines come in, power transformers step the voltage down, and distribution feeders radiate outward to deliver electricity through additional step-down transformers closer to the customer.

### Key Components

Inside a typical distribution substation you will find:

- **Power transformers** that perform the voltage conversion
- **Circuit breakers** that interrupt fault current
- **Disconnect switches** for isolation during maintenance
- **Protective relays** that monitor current, voltage, and frequency
- **Voltage regulators** and **capacitor banks** for power quality
- **A control house** containing the RTAC/RTU and communication equipment

The protective relay system is the backbone of substation safety. It continuously monitors electrical conditions and automatically trips breakers to isolate faulted sections. Getting this protection coordination right is critical. If the wrong breaker trips, or if a breaker fails to trip, you get equipment damage or cascading outages.

### Automation and Communication

Modern substations are heavily automated. An RTAC or RTU talks to every relay, recloser, and regulator over serial or Ethernet links using **DNP3** and **Modbus**. The RTAC reports status upstream to the utility SCADA system and receives operator commands for switching, voltage control, and fault isolation. This communication network is a high-value target for attackers because compromising it gives direct access to breaker controls, protection settings, and voltage regulation.

### How This Maps to the Lab

In the RangerDanger lab, the substation model includes:

- An **RTAC** at 10.30.30.20
- A **protective relay** at 10.40.40.20
- A **recloser** at 10.40.40.21
- A **voltage regulator** at 10.40.40.22
- A **physics simulation engine** at 10.50.50.20

These components communicate over Modbus TCP (port 502) and DNP3 TCP (port 20000), reproducing the traffic patterns you would see on a real substation LAN. The containd NGFW enforces zone boundaries between enterprise, vendor, OT operations, and field device networks, just as a properly segmented utility would deploy them.