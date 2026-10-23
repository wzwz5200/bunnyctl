#!/usr/bin/env python3
"""End-to-end UI test: preset click -> device, save button -> flash, factory restore."""
import json
import sys
import time
import urllib.request

from ffdrive import Marionette

API = "http://127.0.0.1:8099"


def get(path):
    return json.load(urllib.request.urlopen(API + path, timeout=10))


def bands_of(bank="dac"):
    return [(b["freq"], b["gain"], b["q"], b["type"]) for b in get(f"/api/eq/{bank}")["bands"]]


def main():
    m = Marionette()
    m.cmd("WebDriver:NewSession", {"capabilities": {"alwaysMatch": {"browserName": "firefox"}}})
    m.cmd("WebDriver:Navigate", {"url": API + "/"})
    time.sleep(2)

    print("initial :", bands_of())

    # click the "低频增强" preset (3rd chip)
    m.js("document.querySelectorAll('.chip')[2].click()")
    time.sleep(1.2)
    after = bands_of()
    print("preset  :", after)
    m.cmd("WebDriver:TakeScreenshot", {"full": False, "hash": False})
    time.sleep(0.1)

    # save to flash through the UI button
    m.js("document.getElementById('btnSave').click()")
    time.sleep(8)
    print("saved   :", bands_of())
    print("status  :", m.js("return document.getElementById('statusText').textContent"))

    # restore factory (confirm() must be stubbed under Marionette)
    m.js("window.confirm = () => true; document.getElementById('btnFactory').click()")
    time.sleep(9)
    print("factory :", bands_of())
    print("badge   :", m.js("return document.getElementById('unsaved').className"))


if __name__ == "__main__":
    main()
