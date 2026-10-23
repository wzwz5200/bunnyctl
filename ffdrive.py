#!/usr/bin/env python3
"""Tiny Firefox Marionette client: navigate, run JS, screenshot."""
import base64
import json
import socket
import sys
import time


class Marionette:
    def __init__(self, host="127.0.0.1", port=2828):
        self.s = socket.create_connection((host, port), timeout=30)
        self.f = self.s.makefile("rb")
        self._read_msg()  # handshake
        self.id = 0

    def _read_msg(self):
        n = b""
        while True:
            c = self.f.read(1)
            if c == b":":
                break
            n += c
        return json.loads(self.f.read(int(n)))

    def _send(self, msg):
        payload = json.dumps(msg).encode()
        self.s.sendall(str(len(payload)).encode() + b":" + payload)

    def cmd(self, name, params=None):
        self.id += 1
        self._send([0, self.id, name, params or {}])
        while True:
            resp = self._read_msg()
            if resp[0] == 1 and resp[1] == self.id:
                if resp[2]:
                    raise RuntimeError(f"{name}: {resp[2]}")
                return resp[3]

    def js(self, script, args=None):
        return self.cmd("WebDriver:ExecuteScript", {"script": script, "args": args or []})["value"]


def main():
    url = sys.argv[1] if len(sys.argv) > 1 else "http://127.0.0.1:8099/"
    out = sys.argv[2] if len(sys.argv) > 2 else "shot.png"
    m = Marionette()
    m.cmd("WebDriver:NewSession", {"capabilities": {"alwaysMatch": {"browserName": "firefox"}}})
    m.cmd("WebDriver:Navigate", {"url": url})
    time.sleep(2.0)
    shot = m.cmd("WebDriver:TakeScreenshot", {"full": False, "hash": False})
    open(out, "wb").write(base64.b64decode(shot["value"]))
    print("saved", out)
    print("state:", m.js("return JSON.stringify({bands: document.querySelectorAll('.band').length, chips: document.querySelectorAll('.chip').length, status: document.getElementById('statusText').textContent})"))


if __name__ == "__main__":
    main()
