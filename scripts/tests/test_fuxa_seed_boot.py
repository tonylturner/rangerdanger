"""Opt-in isolated image acceptance; not part of unittest discovery.

Usage: python3 scripts/tests/test_fuxa_seed_boot.py rdfuxa-seed-x
Optional real-RTAC check (no live service replacement):
    python3 scripts/tests/test_fuxa_seed_boot.py rdfuxa-seed-x \
        --live-client-image <local image with python3>
"""
from __future__ import annotations

import argparse
import hashlib
import json
import sqlite3
import subprocess
import tempfile
from pathlib import Path
from uuid import uuid4

SERVER = "/usr/src/app/FUXA/server"
BOOT = r"""
const assert = require('node:assert/strict');
const {spawn} = require('node:child_process');
const fs = require('node:fs');
const path = require('node:path');
const lock = require('/opt/rangerdanger/fuxa/plugin/package-lock.json');
for (const [location, entry] of Object.entries(lock.packages)) {
    if (!location) continue;
    const installed = JSON.parse(fs.readFileSync(
        path.join(process.cwd(), location, 'package.json'), 'utf8'));
    assert.equal(installed.version, entry.version, location);
}
assert.ok(fs.statSync('node_modules/modbus-serial').isDirectory());
assert.ok(!fs.lstatSync('node_modules/modbus-serial').isSymbolicLink());
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

# Exercise the full runtime and viewer subscription over container-local
# loopback, including a device that starts late. Only the temporary test project
# points to the fixture. Independent RTAC register map, no write handler.
READ_VALUES = r"""
const assert = require('node:assert/strict');
const net = require('node:net');
const {once} = require('node:events');
const {spawn, execFileSync} = require('node:child_process');
// A separate importer process drains SQLite's close work before FUXA starts.
execFileSync('node', ['-e', `
    const fs = require('node:fs');
    const project = require('./runtime/project');
    const storage = require('./runtime/project/prjstorage');
    const seed = JSON.parse(fs.readFileSync(
        '/opt/rangerdanger/fuxa/starter.fuxap', 'utf8'));
    seed.devices.rd_rtac.property.address = '127.0.0.1:15020';
    (async () => {
        try {
            await project.init({workDir: process.cwd() + '/_appdata'},
                {info() {}, error: console.error}, {});
            await project.setProject(seed);
        } finally { storage.close(); }
    })().catch(error => {console.error(error); process.exitCode = 1;});
`], {timeout: 10000});
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
const child = spawn('node', ['main.js'], {stdio: ['ignore', 'pipe', 'pipe']});
let logs = '';
for (const stream of [child.stdout, child.stderr]) {
    stream.on('data', bytes => { logs += bytes; process.stdout.write(bytes); });
}
const timer = setTimeout(() => {
    console.error('FAIL: viewer did not receive both complete sets within 40 seconds');
    child.kill('SIGKILL');
    process.exit(1);
}, 40000);
let socketUrl;
async function request(url, body) {
    const response = await fetch(url, {
        method: body === undefined ? 'GET' : 'POST', body,
        headers: {'Content-Type': 'text/plain;charset=UTF-8'},
        signal: AbortSignal.timeout(5000)
    });
    assert.ok(response.ok, `HTTP ${response.status}`);
    return response.text();
}
(async () => {
    try {
        // Do not make the fixture available until the real runtime failed to
        // connect. This models FUXA preceding the RTAC after a package switch.
        for (let i = 0; i < 100 && !logs.includes('ECONNREFUSED'); i++) {
            assert.equal(child.exitCode, null, 'FUXA exited before ready');
            await new Promise(resolve => setTimeout(resolve, 100));
        }
        assert.ok(logs.includes('ECONNREFUSED'), 'late-start failure not exercised');
        server.listen(15020, '127.0.0.1');
        await once(server, 'listening');
        const loaded = JSON.parse(await request('http://127.0.0.1:1881/api/project'));
        const tags = Object.values(loaded.hmi.views[0].items).map(item => {
            assert.equal(item.type, 'svg-ext-value');
            assert.deepEqual(item.property.events, []);
            return item.property.variableId;
        });
        assert.equal(tags.length, 6);
        const base = 'http://127.0.0.1:1881/socket.io/?EIO=4&transport=polling';
        const opened = await request(base);
        assert.ok(opened.startsWith('0'));
        socketUrl = base + '&sid=' + JSON.parse(opened.slice(1)).sid;
        await request(socketUrl, '40');
        assert.ok((await request(socketUrl)).startsWith('40'));
        await request(socketUrl, '42' + JSON.stringify(['device-tags-subscribe',
            {tagsId: tags, sendLastValue: true}]));
        const expected = [
            {t_breaker: 1, t_recloser: 1, t_reclose: 1, t_tap: -2,
             t_voltage: 124.7, t_current: 32.1},
            {t_breaker: 0, t_recloser: 0, t_reclose: 1, t_tap: -1,
             t_voltage: 118.9, t_current: 0},
        ];
        while (phase < expected.length) {
            for (const packet of (await request(socketUrl)).split('\x1e')) {
                if (packet === '2') {
                    await request(socketUrl, '3');
                } else if (packet.startsWith('42')) {
                    const [event, message] = JSON.parse(packet.slice(2));
                    if (event !== 'device-values' || message.id !== 'rd_rtac') continue;
                    if (phase === expected.length) continue;
                    const values = Object.fromEntries(
                        message.values.map(tag => [tag.id, tag.value]));
                    if (Object.keys(values).length !== 6) continue;
                    // Ignore the cached first snapshot while awaiting phase 2.
                    if (phase === 1 && values.t_breaker === 1) continue;
                    assert.deepEqual(values, expected[phase]);
                    phase++;
                }
            }
        }
        assert.ok(requests.some(([fc, start]) => fc === 3 && start === 0));
        assert.ok(requests.some(([fc, start]) => fc === 4 && start === 0));
        assert.ok(requests.some(([fc, start, count]) =>
            fc === 4 && start <= 3 && start + count > 3));
        console.log('PASS: full viewer path, late RTAC recovery, six changing values, ' +
            'signed tap, scaling, read-only requests');
    } finally {
        try {
            if (socketUrl) await request(socketUrl, '41');
        } finally {
            child.kill('SIGTERM');
            setTimeout(() => child.kill('SIGKILL'), 2000).unref();
            server.close();
            clearTimeout(timer);
        }
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
        run(["node", "-e", READ_VALUES], 60)
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


def check_live(image: str, client_image: str):
    """Use the live HMI's namespace only as a read-only RTAC client.

    No mounts, published host ports, gateway entrypoint, network attachment
    changes, or writes to the live FUXA project. Both containers are disposable.
    """
    runtime_name = "rdfuxa-live-runtime-" + uuid4().hex[:12]
    client_name = "rdfuxa-live-client-" + uuid4().hex[:12]
    for local_image in (image, client_image):
        subprocess.run(["docker", "image", "inspect", local_image],
                       stdout=subprocess.DEVNULL, check=True, timeout=15)
    try:
        subprocess.run([
            "docker", "run", "--rm", "--detach", "--pull=never",
            "--name", runtime_name,
            "--network", "container:rangerdanger-fuxa-hmi",
            "--entrypoint", "/bin/sh", image, "-c",
            "node /opt/rangerdanger/fuxa/install.js && exec node main.js --port 1882",
        ], check=True, timeout=20)
        # The separate port belongs only to this temporary runtime. Do not run
        # set-gateway.sh in a namespace shared with the live range.
        ready = r"""
        (async () => {
            for (let i = 0; i < 60; i++) {
                try {
                    const r = await fetch('http://127.0.0.1:1882/api/project',
                        {signal: AbortSignal.timeout(500)});
                    if (r.ok) return;
                } catch {}
                await new Promise(resolve => setTimeout(resolve, 200));
            }
            throw new Error('isolated FUXA did not become ready');
        })().catch(error => {console.error(error); process.exitCode = 1;});
        """
        subprocess.run(["docker", "exec", runtime_name, "node", "-e", ready],
                       check=True, timeout=50)
        # Reuse the exact live regression rather than maintaining another
        # Socket.IO implementation. debug() treats a skip as a failure here.
        runner = (
            "import sys; scope={'__name__':'fuxa_live'};"
            "exec(sys.stdin.read(),scope);"
            "scope['FuxaSeedLiveTests']("
            "'test_viewer_receives_six_fresh_read_only_values').debug()"
        )
        subprocess.run([
            "docker", "run", "--rm", "--interactive", "--pull=never",
            "--name", client_name, "--network", f"container:{runtime_name}",
            "--env", "FUXA_LIVE_URL=http://127.0.0.1:1882",
            "--entrypoint", "python3", client_image, "-c", runner,
        ], input=Path(__file__).with_name("test_fuxa_seed_live.py").read_text(),
            text=True, check=True, timeout=40)
        print("PASS: rebuilt isolated FUXA viewer received six fresh real RTAC values")
    finally:
        for name in (client_name, runtime_name):
            result = subprocess.run([
                "docker", "container", "ls", "--all", "--filter",
                f"name=^/{name}$", "--format", "{{.ID}}",
            ], capture_output=True, text=True, check=True, timeout=15)
            if result.stdout.strip():
                subprocess.run(["docker", "rm", "--force", "--volumes", name],
                               check=True, timeout=15)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("image")
    parser.add_argument("--live-client-image")
    args = parser.parse_args()
    if args.live_client_image:
        check_live(args.image, args.live_client_image)
    else:
        check(args.image)
