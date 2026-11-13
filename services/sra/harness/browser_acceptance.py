"""Real Chromium/SAML/OTP/Guacamole acceptance inside the vendor namespace."""
import base64
import hashlib
import hmac
import json
import os
from pathlib import Path
import struct
import subprocess
import sys
import time
from cdp import CDP

BASE = 'http://172.31.250.20:8080/guacamole/'
REALM = json.loads(Path('/tmp/realm.json').read_text())


def totp(secret):
    digest = hmac.new(base64.b32decode(secret), struct.pack('!Q', int(time.time()) // 30), hashlib.sha1).digest()
    offset = digest[-1] & 15
    return f'{(struct.unpack("!I", digest[offset:offset + 4])[0] & 0x7fffffff) % 1000000:06d}'


def login(browser, username):
    browser.call('Page.navigate', url=BASE)
    browser.wait('document.querySelector("#username") !== null')
    browser.js('document.querySelector("#username").value=' + json.dumps(username))
    browser.js('document.querySelector("#password").value="rangerdanger"; document.querySelector("form").requestSubmit()')
    browser.wait('document.querySelector("#otp") !== null')
    user = next(u for u in REALM['users'] if u['username'] == username)
    secret = json.loads(next(c for c in user['credentials'] if c['type'] == 'otp')['secretData'])['value']
    # Avoid crossing a TOTP boundary while submitting.
    if time.time() % 30 > 27:
        time.sleep(3)
    browser.js('document.querySelector("#otp").value=' + json.dumps(totp(secret)) + '; document.querySelector("form").requestSubmit()')
    browser.wait('localStorage.getItem("GUAC_AUTH_TOKEN") !== null')
    auth = {'authToken': browser.js('JSON.parse(localStorage.getItem("GUAC_AUTH_TOKEN"))')}
    print('AUTH', username, {k: v for k, v in auth.items() if k != 'authToken'}, flush=True)
    return auth


def api(browser, path, token):
    expression = f'''(async () => {{ const r=await fetch({json.dumps(BASE + 'api/' + path + '?token=' + token)}); return {{status:r.status, body:await r.text()}}; }})()'''
    result = browser.js(expression)
    if result['status'] != 200:
        raise RuntimeError((path, result))
    return json.loads(result['body'])


def main(mode):
    if mode == 'session':
        assert os.getuid() == 911
        assert 'Seccomp:\t2' in Path('/proc/self/status').read_text()
        print('BROWSER abc UID 911; Docker default seccomp active;',
              subprocess.check_output(['/usr/bin/chromium', '--version'], text=True).strip(), flush=True)
        home = '/tmp/sra-home'
        os.makedirs(home, exist_ok=True)
        environment = dict(os.environ, HOME=home)
        log = open('/tmp/chromium.log', 'w')
        subprocess.Popen(['/usr/bin/chromium', '--headless', '--disable-gpu', '--disable-dev-shm-usage',
                          '--remote-debugging-port=9222', '--user-data-dir=' + home + '/profile', BASE],
                         env=environment, stdout=log, stderr=log)
        for _ in range(50):
            try:
                browser = CDP()
                break
            except (OSError, ValueError):
                time.sleep(.1)
        else:
            raise RuntimeError(Path('/tmp/chromium.log').read_text())
        auth = login(browser, 'vendor')
        token = auth['authToken']
        connections = api(browser, 'session/data/postgresql/connections', token)
        assert len(connections) == 1 and next(iter(connections.values()))['name'] == 'RTAC SSH (brokered)', connections
        direct = browser.js('''(async()=>{const r=await fetch('api/tokens',{method:'POST',headers:{'Content-Type':'application/x-www-form-urlencoded'},body:'username=vendor&password=rangerdanger'});return {status:r.status,body:await r.text()}})()''')
        assert direct['status'] == 403, direct
        identifier = next(iter(connections))
        encoded = base64.b64encode((identifier + '\0c\0postgresql').encode()).decode()
        browser.call('Page.navigate', url=BASE + '#/client/' + encoded)
        browser.wait('document.querySelector(".client .display") !== null')
        time.sleep(8)
        for character in 'whoami > /tmp/rdsra-b-user':
            browser.call('Input.dispatchKeyEvent', type='keyDown', key=character, text=character)
            browser.call('Input.dispatchKeyEvent', type='keyUp', key=character)
        browser.call('Input.dispatchKeyEvent', type='keyDown', key='Enter', code='Enter', windowsVirtualKeyCode=13)
        browser.call('Input.dispatchKeyEvent', type='keyUp', key='Enter', code='Enter', windowsVirtualKeyCode=13)
        time.sleep(2)
        print('CLIENT TEXT', browser.js('document.body.innerText'), flush=True)
        Path('/tmp/vendor-auth.json').write_text(json.dumps(auth))
        print('SESSION ACCEPTED', flush=True)
    elif mode == 'audit':
        browser = CDP()
        old = json.loads(Path('/tmp/vendor-auth.json').read_text())['authToken']
        # The disconnected client's SAML redirect may already be on Keycloak.
        # Use an inert same-origin resource, not the login-redirecting app.
        browser.call('Page.navigate', url=BASE + 'images/logo-64.png')
        browser.wait('location.pathname === "/guacamole/images/logo-64.png" && document.readyState === "complete"')
        result = browser.js(f'''(async()=>{{const r=await fetch({json.dumps(BASE+'api/session/data/postgresql/connections?token='+old)});return r.status}})()''')
        assert result in (401, 403), result
        print('PASS: pre-reset auth token rejected', result, flush=True)
        browser.js('localStorage.clear()')
        browser.call('Network.clearBrowserCookies')
        auth = login(browser, 'auditor')
        token = auth['authToken']
        assert not api(browser, 'session/data/postgresql/connections', token)
        history = api(browser, 'session/data/postgresql/history/connections', token)
        print('HISTORY', history, flush=True)
        assert history and all(h.get('endDate') is not None for h in history), history
        recordings = [h for h in history if h.get('logs')]
        if not recordings:
            raise RuntimeError('Contract blocker: auditor with system AUDIT and no connection '
                               'permissions lists history but cannot access recording logs. '
                               'Guacamole 1.6.0 recording-storage requires system ADMINISTER '
                               'or connection UPDATE for playback; AUDIT alone is insufficient.')
        recording = recordings[0]
        log_name = next(iter(recording['logs']))
        from urllib.parse import quote
        player_url = BASE + '#/settings/postgresql/recording/' + quote(recording['identifier'], safe='') + '/' + quote(log_name, safe='')
        browser.call('Page.navigate', url=player_url)
        browser.wait('document.querySelector(".guac-player-seek") && Number(document.querySelector(".guac-player-seek").max) > 0')
        browser.js('document.querySelector(".guac-player-play").click()')
        browser.wait('document.querySelector(".guac-player-controls.playing") !== null')
        browser.wait('Number(document.querySelector(".guac-player-seek").value) > 0')
        print('AUDIT PLAYBACK ACCEPTED', browser.js('document.querySelector(".guac-player-position").innerText'), flush=True)


if __name__ == '__main__':
    main(sys.argv[1])
