"""Minimal stdlib Chrome DevTools client; runs inside the disposable vendor."""
import base64
import hashlib
import json
import os
import socket
import struct
import time
import urllib.request


class CDP:
    def __init__(self):
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
        with opener.open('http://127.0.0.1:9222/json/list', timeout=5) as response:
            page = next(p for p in json.load(response) if p['type'] == 'page')
        from urllib.parse import urlsplit
        address = urlsplit(page['webSocketDebuggerUrl'])
        self.sock = socket.create_connection((address.hostname, address.port), timeout=15)
        key = base64.b64encode(os.urandom(16)).decode()
        self.sock.sendall((f'GET {address.path} HTTP/1.1\r\nHost: localhost\r\nUpgrade: websocket\r\n'
                           f'Connection: Upgrade\r\nSec-WebSocket-Key: {key}\r\nSec-WebSocket-Version: 13\r\n\r\n').encode())
        header = b''
        while not header.endswith(b'\r\n\r\n'):
            header += self.sock.recv(1)
        expected = base64.b64encode(hashlib.sha1((key + '258EAFA5-E914-47DA-95CA-C5AB0DC85B11').encode()).digest())
        assert b' 101 ' in header and expected in header, header
        self.sequence = 0
        self.call('Page.enable')

    def read(self, count):
        output = b''
        while len(output) < count:
            part = self.sock.recv(count - len(output))
            if not part:
                raise EOFError('CDP socket closed')
            output += part
        return output

    def call(self, method, **params):
        self.sequence += 1
        payload = json.dumps({'id': self.sequence, 'method': method, 'params': params}).encode()
        size = len(payload)
        prefix = bytes([0x81, 0x80 | size]) if size < 126 else bytes([0x81, 0x80 | 126]) + struct.pack('!H', size)
        mask = os.urandom(4)
        self.sock.sendall(prefix + mask + bytes(c ^ mask[i % 4] for i, c in enumerate(payload)))
        while True:
            opcode, size = self.read(2)
            if size == 126:
                size = struct.unpack('!H', self.read(2))[0]
            elif size == 127:
                size = struct.unpack('!Q', self.read(8))[0]
            message = json.loads(self.read(size))
            if message.get('id') == self.sequence:
                if 'error' in message:
                    raise RuntimeError(message)
                return message.get('result', {})

    def js(self, expression):
        result = self.call('Runtime.evaluate', expression=expression, returnByValue=True, awaitPromise=True)
        if 'exceptionDetails' in result:
            raise RuntimeError(result)
        return result.get('result', {}).get('value')

    def wait(self, expression, limit=40):
        end = time.monotonic() + limit
        while time.monotonic() < end:
            value = self.js(expression)
            if value:
                return value
            time.sleep(.2)
        raise TimeoutError(f'{expression}: ' + str(self.js('document.body.innerText')))
