'use strict';

const fs = require('node:fs');
const path = require('node:path');
const crypto = require('node:crypto');
const server = '/usr/src/app/FUXA/server';
const project = require(`${server}/runtime/project`);
const storage = require(`${server}/runtime/project/prjstorage`);

async function bake() {
    const workDir = fs.mkdtempSync(path.join(__dirname, '.bake-'));
    const logger = { info() {}, error(message) { console.error(message); } };
    try {
        await project.init({ workDir }, logger, {});
        await project.setProject(JSON.parse(fs.readFileSync(
            path.join(__dirname, 'starter.fuxap'), 'utf8')));
    } finally {
        // sqlite3 drains pending close work before Node exits.
        storage.close();
    }
    const bytes = fs.readFileSync(path.join(workDir, 'project.fuxap.db'));
    fs.writeFileSync(path.join(__dirname, 'project.fuxap.db'), bytes);
    fs.writeFileSync(path.join(__dirname, 'project.sha256'),
        crypto.createHash('sha256').update(bytes).digest('hex') + '\n');
    fs.rmSync(workDir, { recursive: true });
}

bake().catch(error => {
    console.error(error);
    process.exitCode = 1;
});
