# US FUXA starter

The image imports `starter.fuxap` using the pinned FUXA project's own
SQLite schema at build time. At boot, the entrypoint verifies the baked
database's SHA-256 and installs it into `_appdata/project.fuxap.db` only
when **both `_appdata` and `_db` are empty or absent**. Any existing data,
including settings-only or damaged projects, is preserved byte-for-byte.
`_logs` does not affect installation. Restarting never upgrades or resets
a user project. Back up user data before deliberately removing it to reseed.
No download or remote service is needed to install the starter.
The base image omits the optional Modbus transport plugin. Its isolated
`plugin/package.json` and `plugin/package-lock.json` lock `modbus-serial`
8.0.19 (the pinned FUXA registry's version) and every transitive package
with registry integrity hashes. The Dockerfile uses `npm ci` with nested
installation, then moves the complete self-contained `modbus-serial`
directory into FUXA's `server/node_modules`. FUXA's normal package scanner
discovers that directory and its adapter's `require('modbus-serial')`
resolves it; plugin dependencies resolve inside its nested `node_modules`,
not from FUXA's other packages. No symlink, runtime download, or manifest
rewrite is needed. The build verifies that FUXA's original `package.json`
and `package-lock.json` are unchanged.

To intentionally refresh the lock, run `npm install --package-lock-only
--install-strategy=nested --ignore-scripts --no-audit --no-fund --prefix
lab-definitions/packages/us-dnp3-substation/seeds/fuxa/plugin` and review
the generated lock. Normal builds never update it.

The **US substation — RTAC monitor** view reads Modbus TCP from
`10.30.30.20:502`, unit 1, every two seconds. It adds no inter-zone flow.
It contains no command widgets or write events. FUXA remains an editable
lab tool, not a read-only security boundary.

| Display | RTAC function / zero-based register | FUXA address / type |
|---|---|---|
| Breaker closed | FC3 / 0 | 1 / UInt16 |
| Recloser closed | FC3 / 1 | 2 / UInt16 |
| Auto-reclose enabled | FC3 / 2 | 3 / UInt16 |
| Regulator tap | FC3 / 3 | 4 / Int16 |
| Bus voltage (V) | FC4 / 0 | 1 / UInt16, divisor 10 |
| Feeder current (A) | FC4 / 3 | 4 / UInt16, divisor 10 |

The register authority is `services/rtac-sim/modbus.go`. FUXA uses a
separate memory selector (400000 holding, 300000 input) and one-based
addresses; it subtracts one before sending the request. Values remain
unknown until the RTAC is reachable; the view does not seed live values.
Widgets bind to bare tag IDs (for example `t_breaker`), exactly as emitted
by the pinned server's `device-values` event. The viewer subscribes with
those same IDs; a legacy `device^~^tag` binding receives no values with
FUXA's default `broadcastAll: false`. Device connection retries are handled
by FUXA itself, including when the RTAC starts after FUXA.
The fixed-size native FUXA canvas has no scaling viewBox.

This optional monitor is not the workshop's primary HMI. Use
`/substation` for the feeder one-line, alarm chain and customer-service
evidence. Existing `hmi_poller` traffic is separate from this project.

Validation:

```sh
python3 -m unittest scripts.tests.test_fuxa_seed
python3 -m unittest scripts.tests.test_fuxa_seed_live
docker build -f Dockerfile.fuxa-hmi -t rdfuxa-seed-x .
python3 scripts/tests/test_fuxa_seed_boot.py rdfuxa-seed-x
# Optional: US range running, with an already-local Python client image.
python3 scripts/tests/test_fuxa_seed_boot.py rdfuxa-seed-x \
  --live-client-image rangerdanger-eng_workstation
docker image rm rdfuxa-seed-x
```

The boot check uses isolated, automatically removed, network-none
containers and temporary data mounts; it never starts the range.
It also starts the full FUXA runtime before its loopback Modbus fixture
and checks the viewer's Socket.IO subscription after connection recovery.
The live check is read-only and skips when no US FUXA service is running.
It loads the actual project, subscribes using its widget bindings, and
requires all six numeric values on two successive polls with newer timestamps.
Set `FUXA_LIVE_URL` to check an isolated container's HTTP endpoint instead.
The opt-in image check reuses that same test against a freshly seeded,
disposable FUXA runtime on port 1882 in the live HMI's network namespace,
reading the real RTAC. It publishes no host ports, mounts no live data,
does not run the gateway entrypoint, and removes both of its containers.
It never swaps or restarts a live service or changes its network settings.
