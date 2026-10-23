#!/usr/bin/env python3
"""ktdump — dump the internal flash of a KTMicro DSP dongle over HID 0x08 reads.

Findings on TANCHJIM BUNNY DSP (31b2:1112, fw 1.0.1):
  - `0x08 <addr LE32>` returns one 32-bit word in response bytes 1..4
  - addr 0x80000 maps to flash offset 0 (image header "KT_l..." = KT_lnv1b_flash_1)
  - **only 4-byte aligned addresses are safe** — an unaligned address wedges the
    firmware's HID pipe until the dongle is physically replugged.

Usage:
  ./ktdump.py --check                    # probe a few aligned addresses
  ./ktdump.py -o flash.bin               # full 256 KiB dump (resumable)
  ./ktdump.py -o flash.bin --start 0x1000 --size 0x1000
"""

import argparse
import json
import os
import select
import struct
import sys
import time

FLASH_BASE = 0x80000
FLASH_SIZE = 0x40000          # 2 Mbit = 256 KiB
RID = 0x4B
PROGRESS = ".ktdump.progress.json"


class Dev:
    def __init__(self, path):
        self.fd = os.open(path, os.O_RDWR | os.O_NONBLOCK)

    def close(self):
        os.close(self.fd)

    def word(self, addr, timeout=0.25):
        os.write(self.fd, bytes([RID]) + struct.pack("<I", addr) + bytes([0x08, 0]) + b"\0\0\0\0")
        end = time.time() + timeout
        while time.time() < end:
            r, _, _ = select.select([self.fd], [], [], end - time.time())
            if not r:
                return None
            d = os.read(self.fd, 64)
            if len(d) >= 5 and d[0] == RID:
                return struct.unpack("<I", d[1:5])[0]
        return None


def find_dev():
    for e in sorted(os.listdir("/sys/class/hidraw")):
        try:
            txt = open(f"/sys/class/hidraw/{e}/device/uevent").read().upper()
        except OSError:
            continue
        if "31B2" in txt:
            return f"/dev/{e}"
    return None


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("-o", "--out", default="flash.bin")
    ap.add_argument("--start", type=lambda x: int(x, 0), default=FLASH_BASE)
    ap.add_argument("--size", type=lambda x: int(x, 0), default=FLASH_SIZE)
    ap.add_argument("--check", action="store_true")
    ap.add_argument("--dev", default=None)
    ap.add_argument("--fresh", action="store_true", help="ignore resume state")
    args = ap.parse_args()

    path = args.dev or find_dev()
    if not path:
        sys.exit("no 31b2:xxxx HID device found")
    dev = Dev(path)
    print(f"device {path}")

    if args.check:
        for addr in (0x80000, 0x80004, 0x80008, 0x83000, 0x83FFC, 0xBFFFC):
            v = dev.word(addr)
            print(f"0x{addr:06X}: {v if v is None else '0x%08X  %r' % (v, struct.pack('<I', v))}")
        return

    if args.start % 4 or args.size % 4:
        sys.exit("start/size must be 4-byte aligned (unaligned reads wedge the device)")

    buf = bytearray(b"\0" * args.size)
    done = 0
    if not args.fresh and os.path.exists(PROGRESS):
        st = json.load(open(PROGRESS))
        if st.get("start") == args.start and st.get("size") == args.size and os.path.exists(args.out):
            buf[:] = open(args.out, "rb").read()[:args.size]
            done = st["done"]
            print(f"resume at 0x{args.start + done:06X} ({done}/{args.size})")

    t0 = time.time()
    addrs = range(done, args.size, 4)
    fails = 0
    for i in addrs:
        v = dev.word(args.start + i)
        if v is None:
            time.sleep(0.05)
            v = dev.word(args.start + i, 0.5)
        if v is None:
            fails += 1
            print(f"\n!! timeout @0x{args.start + i:06X} — aborting, partial data kept")
            break
        buf[i:i + 4] = struct.pack("<I", v)
        if (i // 4) % 1024 == 0:
            open(args.out, "wb").write(bytes(buf[:i + 4]))
            json.dump({"start": args.start, "size": args.size, "done": i + 4}, open(PROGRESS, "w"))
            el = time.time() - t0
            print(f"\r{i + 4:6d}/{args.size}  {100 * (i + 4) / args.size:5.1f}%  "
                  f"{el:5.1f}s", end="", flush=True)
    open(args.out, "wb").write(bytes(buf[:i + 4]))
    print(f"\nwrote {args.out} ({i + 4} bytes), fails={fails}")
    print(f"header: {bytes(buf[:16])!r}")


if __name__ == "__main__":
    main()
