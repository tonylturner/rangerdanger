'use strict';

const fs = require('node:fs');
const path = require('node:path');
const crypto = require('node:crypto');

function install(root = '/usr/src/app/FUXA/server', seedDir = __dirname) {
    const appdata = path.join(root, '_appdata');
    const database = path.join(root, '_db');
    // Be conservative: even settings-only, legacy, or damaged user data belongs
    // to the user. Never infer that an existing project is safe to replace.
    for (const directory of [appdata, database]) {
        if (fs.existsSync(directory) && fs.readdirSync(directory).length) {
            return 'existing data; starter not installed';
        }
    }
    const bytes = fs.readFileSync(path.join(seedDir, 'project.fuxap.db'));
    const expected = fs.readFileSync(path.join(seedDir, 'project.sha256'), 'utf8').trim();
    const actual = crypto.createHash('sha256').update(bytes).digest('hex');
    if (actual !== expected) {
        throw new Error('FUXA starter checksum mismatch');
    }
    fs.mkdirSync(appdata, { recursive: true });
    const temporary = fs.mkdtempSync(path.join(appdata, '.seed-'));
    try {
        const staged = path.join(temporary, 'project.fuxap.db');
        fs.writeFileSync(staged, bytes);
        // Atomic publication without replacement (unlike rename).
        fs.linkSync(staged, path.join(appdata, 'project.fuxap.db'));
    } finally {
        fs.rmSync(temporary, { recursive: true });
    }
    return 'starter installed';
}

module.exports = { install };
if (require.main === module) {
    console.log(`fuxa-seed: ${install()}`);
}
