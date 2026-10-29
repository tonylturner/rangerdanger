"""Opt-in isolated image acceptance; not part of unittest discovery.

Usage: python3 scripts/tests/test_fuxa_seed_boot.py rdfuxa-seed-x
"""
from __future__ import annotations

import hashlib
import json
import sqlite3
import subprocess
import sys
import tempfile
from pathlib import Path
from uuid import uuid4

SERVER = "/usr/src/app/FUXA/server"
BOOT = r"""
const assert = require('node:assert/strict');
const {spawn} = require('node:child_process');
assert.equal(require('modbus-serial/package.json').version, '8.0.19');
const child = spawn('node', ['main.js'], {stdio: ['ignore', 'pipe', 'pipe']});
let logs = '';
for (const stream of [child.stdout, child.stderr]) {
    stream.on('data', bytes => { logs += bytes; process.stdout.write(bytes); });
}
const timer = setTimeout(() => { child.kill('SIGKILL'); process.exit(1); }, 45000);
(async () => {
    try {
        let loaded;
        for (let i = 0; i < 100; i++) {
            if (child.exitCode !== null) throw new Error('FUXA exited before ready');
            try {
                const response = await fetch('http://127.0.0.1:1881/api/project',
                    {signal: AbortSignal.timeout(1000)});
                if (response.ok) { loaded = await response.json(); break; }
            } catch {}
            await new Promise(resolve => setTimeout(resolve, 300));
        }
        assert.ok(loaded, 'project API did not become ready');
        const project = loaded;
        assert.equal(project.hmi.views[0].id, 'rd_us_monitor');
        assert.equal(project.hmi.layout.start, 'rd_us_monitor');
        assert.equal(project.devices.rd_rtac.property.address, '10.30.30.20:502');
        assert.equal(Object.keys(project.devices.rd_rtac.tags).length, 6);
        assert.ok(logs.includes("'US RTAC' created"), 'Modbus device not instantiated');
        assert.ok(!logs.includes('plugin is missing'), 'required device plugin missing');
        console.log('PASS: FUXA loaded the starter device and view');
    } catch (error) {
        console.error(error);
        process.exitCode = 1;
    } finally {
        clearTimeout(timer);
        child.kill('SIGTERM');
        setTimeout(() => child.kill('SIGKILL'), 2000).unref();
    }
})();
"""

# Exercise the actual pinned adapter/transport over container-local loopback.
# The exported project's frozen address is unchanged; only this in-memory test
# device points to the fixture. Independent RTAC register map, no write handler.
READ_VALUES = r"""
const assert = require('node:assert/strict');
const net = require('node:net');
const {once, EventEmitter} = require('node:events');
const fs = require('node:fs');
const adapter = require('./runtime/devices/modbus');
const device = JSON.parse(fs.readFileSync(
    '/opt/rangerdanger/fuxa/starter.fuxap', 'utf8')).devices.rd_rtac;
let phase = 0;
const requests = [];
const server = net.createServer(socket => {
    let pending = Buffer.alloc(0);
    socket.on('data', bytes => {
        pending = Buffer.concat([pending, bytes]);
        while (pending.length >= 6 && pending.length >= 6 + pending.readUInt16BE(4)) {
            const size = 6 + pending.readUInt16BE(4);
            const request = pending.subarray(0, size);
            pending = pending.subarray(size);
            const fc = request[7], start = request.readUInt16BE(8);
            const count = request.readUInt16BE(10);
            assert.equal(request[6], 1);
            assert.ok(fc === 3 || fc === 4, 'only Modbus reads are permitted');
            requests.push([fc, start, count]);
            const registers = fc === 3
                ? (phase ? [0, 0, 1, 65535] : [1, 1, 1, 65534])
                : (phase ? [1189, 0, 0, 0] : [1247, 0, 0, 321]);
            assert.ok(start + count <= registers.length);
            const response = Buffer.alloc(9 + count * 2);
            request.copy(response, 0, 0, 7);
            response.writeUInt16BE(3 + count * 2, 4);
            response[7] = fc;
            response[8] = count * 2;
            for (let i = 0; i < count; i++) {
                response.writeUInt16BE(registers[start + i], 9 + i * 2);
            }
            socket.write(response);
        }
    });
});
const timer = setTimeout(() => process.exit(1), 15000);
(async () => {
    server.listen(0, '127.0.0.1');
    await once(server, 'listening');
    device.property.address = `127.0.0.1:${server.address().port}`;
    const errors = [];
    const logger = {info() {}, error(message) {errors.push(message);} };
    const client = adapter.create(device, logger, new EventEmitter(), null, {});
    assert.ok(client, 'Modbus transport missing');
    client.init(adapter.ModbusTypes.TCP);
    client.load(device);
    try {
        await client.connect();
        for (const expected of [
            {t_breaker: 1, t_recloser: 1, t_reclose: 1, t_tap: -2,
             t_voltage: 124.7, t_current: 32.1},
            {t_breaker: 0, t_recloser: 0, t_reclose: 1, t_tap: -1,
             t_voltage: 118.9, t_current: 0},
        ]) {
            await client.polling();
            for (const [key, value] of Object.entries(expected)) {
                assert.equal(client.getValue(key)?.value, value, key);
            }
            phase++;
        }
        assert.deepEqual(errors, []);
        assert.ok(requests.some(([fc, start]) => fc === 3 && start === 0));
        assert.ok(requests.some(([fc, start]) => fc === 4 && start === 0));
        assert.ok(requests.some(([fc, start, count]) =>
            fc === 4 && start <= 3 && start + count > 3));
        console.log('PASS: six changing values, signed tap, scaling, read-only requests');
    } finally {
        await client.disconnect();
        await new Promise(resolve => server.close(resolve));
        clearTimeout(timer);
    }
})().catch(error => { console.error(error); process.exitCode = 1; });
"""


def check(image: str):
    with tempfile.TemporaryDirectory(prefix="rdfuxa-seed-") as directory:
        root = Path(directory)
        mounts = []
        for name in ("_appdata", "_db", "_logs"):
            (root / name).mkdir()
            mounts.extend(["-v", f"{root / name}:{SERVER}/{name}"])

        def run(arguments: list[str], timeout: int):
            name = "rdfuxa-seed-" + uuid4().hex[:12]
            try:
                subprocess.run(
                    ["docker", "run", "--rm", "--network", "none",
                     "--name", name, *mounts, image, *arguments],
                    check=True, timeout=timeout,
                )
            finally:
                # Killing a timed-out Docker client need not stop its container.
                result = subprocess.run(
                    ["docker", "container", "ls", "--all", "--filter",
                     f"name=^/{name}$", "--format", "{{.ID}}"],
                    capture_output=True, text=True, check=True, timeout=15,
                )
                if result.stdout.strip():
                    subprocess.run(["docker", "rm", "--force", name],
                                   check=True, timeout=15)

        run(["node", "-e", BOOT], 65)
        run(["node", "-e", READ_VALUES], 30)
        database = root / "_appdata/project.fuxap.db"
        with sqlite3.connect(database) as db:
            views = db.execute("SELECT value FROM views").fetchall()
            assert len(views) == 1
            assert json.loads(views[0][0])["id"] == "rd_us_monitor"
            # Model a saved user edit, then prove entrypoint doesn't replace it.
            db.execute("UPDATE general SET value = ? WHERE name = 'version'",
                       (json.dumps("user-saved"),))
        before = hashlib.sha256(database.read_bytes()).hexdigest()
        run(["node", "-e", "console.log('existing-project boot');"], 30)
        assert hashlib.sha256(database.read_bytes()).hexdigest() == before
        print("PASS: existing project preserved byte-for-byte")
    print("PASS: 3 isolated image acceptance checks")


if __name__ == "__main__":
    check(sys.argv[1])
