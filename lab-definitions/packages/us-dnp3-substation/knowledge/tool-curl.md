Every field device simulator in the lab exposes an HTTP REST API on **port 8080**. `curl` is the simplest way to read device state, send commands, and check health. It is available on the Kali box and most other lab containers.

### Reading Device State

```bash
# Relay state
curl -s http://10.40.40.20:8080/api/state | jq .

# Recloser state
curl -s http://10.40.40.21:8080/api/state | jq .

# Regulator state
curl -s http://10.40.40.22:8080/api/state | jq .

# RTAC aggregated state (all devices)
curl -s http://10.30.30.20:8080/api/state | jq .
```

Pipe through `jq` for readable JSON output. The `-s` flag silences the progress bar.

### Sending Commands

Commands go via POST to `/api/command`:

```bash
# Trip the relay breaker
curl -s -X POST http://10.40.40.20:8080/api/command \
  -H "Content-Type: application/json" \
  -d '{"action": "trip"}'

# Close the relay breaker
curl -s -X POST http://10.40.40.20:8080/api/command \
  -H "Content-Type: application/json" \
  -d '{"action": "close"}'

# Disable recloser auto-reclose
curl -s -X POST http://10.40.40.21:8080/api/command \
  -H "Content-Type: application/json" \
  -d '{"action": "disable-reclose"}'

# Set regulator tap position
curl -s -X POST http://10.40.40.22:8080/api/command \
  -H "Content-Type: application/json" \
  -d '{"action": "set-tap", "value": 10}'
```

### Health Checks

```bash
curl -s http://10.40.40.20:8080/api/health
```

Returns a simple status response. Useful for verifying a device is running before trying protocol-level interaction.

### Audit Log

```bash
curl -s http://10.40.40.20:8080/api/audit | jq .
```

Returns the command audit trail showing all commands the device has received. This is useful for confirming that your Modbus or DNP3 commands actually reached the device and what effect they had.

### When to Use curl vs. Modbus/DNP3

The HTTP API is convenient for quick checks and is not subject to the same firewall rules as industrial protocols. However, the exercises focus on Modbus and DNP3 because those are the protocols used in real substations. Use `curl` for:

- Verifying device state before and after attacks
- Confirming that commands sent via Modbus/DNP3 had the expected effect
- Quick troubleshooting when something is not working as expected