Modbus is one of the oldest and most widely deployed industrial communication protocols, originally developed by Modicon in 1979. Modbus TCP wraps Modbus application data units in TCP/IP packets on **port 502**. Its simplicity and ubiquity make it the default choice for communicating with PLCs, RTUs, meters, and field devices. It is also one of the least secure protocols in widespread use.

### Function Codes

Modbus uses a request-response model with function codes defining the operation:

| Code | Name | Type |
|------|------|------|
| FC01 | Read Coils | Read binary outputs |
| FC02 | Read Discrete Inputs | Read binary inputs |
| FC03 | Read Holding Registers | Read analog values |
| FC04 | Read Input Registers | Read analog inputs |
| FC05 | Write Single Coil | Write binary output |
| FC06 | Write Single Register | Write analog value |
| FC15 | Write Multiple Coils | Write binary outputs |
| FC16 | Write Multiple Registers | Write analog values |

Read operations retrieve data. Write operations change device state. In a substation context, write operations mean opening breakers, changing tap positions, or disabling protection functions.

### Zero Security by Design

:::caution Modbus TCP has no native security
**No authentication, no encryption, no authorization.** Any device that can establish a TCP connection to port 502 can read or write any register. The server has no way to distinguish a legitimate RTAC command from a malicious one sent by an attacker. Modbus was designed in 1979 for isolated serial networks where physical access was the security boundary; deploying it on routable IP networks without compensating controls is inherently dangerous.
:::

### The Compensating Control: Segmentation + DPI

The primary defense for Modbus TCP is network segmentation with protocol-aware firewalling. The containd NGFW inspects Modbus traffic at the application layer and filters by function code. The hardened policy allows FC03 (read holding registers) from the HMI to field devices but blocks FC05 and FC06 (write operations) from any source except the RTAC. This is the best available protection for a protocol with no native security.

### Lab Implementation

Every field device simulator exposes Modbus TCP on port 502:

- **Relay** (10.40.40.20): breaker position, trip count, lockout status in coils and holding registers
- **Recloser** (10.40.40.21): auto-reclose state as a Modbus coil
- **Regulator** (10.40.40.22): tap position as a Modbus holding register

You use `mbpoll` on the Kali box to read and write these registers. The difference between weak and hardened policies is the difference between full read/write access and being blocked at the firewall.

### See Also

- [What is ICS DPI?](#ics-dpi) - function-code filtering is the DPI lesson the lab teaches against Modbus
- [`mbpoll` - Modbus TCP Command-Line Tool](#tool-mbpoll) - the CLI used in the labs
- [DNP3 in Substations](#dnp3) - the protocol DNP3 plays to Modbus's role in distribution