#!/usr/bin/env python3
"""ktdsp — control tool for KTMicro USB audio DSP dongles (TANCHJIM BUNNY DSP).

Protocol (reverse-engineered, see PROTOCOL-FINDINGS.md):
  HID interface 3, report ID 0x4B, 11-byte frames
    read : 4B <addr LE32> 52 00 00 00 00 00
    write: 4B <addr LE32> 57 00 <value LE32>     -> ACK 0x03
    save : 4B 00 00 00 00 53 00 00 00 00 00     -> commits RAM to flash

Pure stdlib (hidraw file I/O); no hidapi/numpy needed.
"""

import argparse
import json
import math
import os
import select
import struct
import sys
import time

KT_VID = 0x31B2
RID = 0x4B
MAGIC = 0x12345678

# ---- register map (Helios / KT02H20-class layout) ----
REG_FLAGS = 0x01
REG_VERSION = 0x04
REG_DATE = 0x08
REG_MANUF = 0x40
REG_PRODUCT = 0x48
REG_SERIAL = 0x50
REG_VIDPID = 0x5B
REG_MAGIC = 0xE1

EQ_ADC_EN, EQ_ADC_BASE = 0x18, 0x1A
EQ_DAC_EN, EQ_DAC_BASE = 0x24, 0x26
EQ_BANDS = 5

PGA_ADC, PGA_DAC = 0x3A, 0x3B
DIG_ADC, DIG_DAC = 0x65, 0x66

NG_BASE, LIM_BASE = 0x71, 0x78

PGA_ADC_GAINS = [0, -6, 8, 14, 20, 26, 32, 44]
PGA_DAC_GAINS = ["mute"] + [1.5 * (i - 1) - 18 for i in range(1, 16)]

TYPE_NAMES = {0: "PK", 1: "LPF", 2: "HPF", 3: "LSH", 4: "HSH"}
TYPE_IDS = {v: k for k, v in TYPE_NAMES.items()}


# ═══════════════════════════════════════════════════════════════
#  transport
# ═══════════════════════════════════════════════════════════════
class KTError(RuntimeError):
    pass


class KTDevice:
    def __init__(self, path=None, pid=None):
        self.path = path or self._find(pid)
        if not self.path:
            raise KTError("no KTMicro HID device found (check udev rule / cable)")
        self.fd = os.open(self.path, os.O_RDWR | os.O_NONBLOCK)

    @staticmethod
    def _find(pid=None):
        for entry in sorted(os.listdir("/sys/class/hidraw")):
            ue = f"/sys/class/hidraw/{entry}/device/uevent"
            try:
                txt = open(ue).read().upper()
            except OSError:
                continue
            if f"{KT_VID:04X}" not in txt:
                continue
            if pid is not None and f"{pid:04X}" not in txt:
                continue
            return f"/dev/{entry}"
        return None

    def close(self):
        if self.fd is not None:
            os.close(self.fd)
            self.fd = None

    def __enter__(self):
        return self

    def __exit__(self, *exc):
        self.close()
        return False

    # ---- framing ----
    def _send(self, frame):
        os.write(self.fd, frame)

    def _recv(self, timeout=0.5):
        r, _, _ = select.select([self.fd], [], [], timeout)
        if not r:
            return None
        try:
            return os.read(self.fd, 64)
        except OSError:
            return None

    def _drain(self):
        while self._recv(0.02):
            pass

    # ---- register ops ----
    def read(self, addr, retries=3):
        for _ in range(retries):
            self._send(bytes([RID]) + struct.pack("<I", addr) + bytes([0x52, 0]) + b"\0\0\0\0")
            end = time.time() + 0.4
            while time.time() < end:
                d = self._recv(end - time.time())
                if d is None:
                    break
                if (len(d) >= 11 and d[0] == RID and d[5] == 0x52
                        and struct.unpack("<I", d[1:5])[0] == addr):
                    return struct.unpack("<I", d[7:11])[0]
        raise KTError(f"read timeout @0x{addr:02X}")

    def write(self, addr, value, verify=True, retries=3):
        for _ in range(retries):
            self._send(bytes([RID]) + struct.pack("<I", addr) + bytes([0x57, 0])
                       + struct.pack("<I", value & 0xFFFFFFFF))
            d = self._recv(0.4)
            if d and len(d) >= 8 and d[5] == 0x57:
                status = struct.unpack("<I", d[7:11])[0]
                if status & 0xFF in (0x03, 0x4F):
                    if verify and self.read(addr) != (value & 0xFFFFFFFF):
                        raise KTError(f"write verify failed @0x{addr:02X}")
                    return status
        raise KTError(f"write timeout @0x{addr:02X}")

    def read_str(self, addr, words):
        out = b""
        for i in range(words):
            out += struct.pack("<I", self.read(addr + i))
        return out.split(b"\0")[0].decode("ascii", "replace")

    def save_flash(self):
        """0x53 'S' — commit live RAM state to flash; device reboots."""
        self._send(bytes([RID]) + b"\0\0\0\0" + bytes([0x53]) + b"\0\0\0\0")
        d = self._recv(0.6)
        if not d:
            return None
        return d.hex()

    # ---- identity ----
    def info(self):
        flags = self.read(REG_FLAGS)
        return {
            "version": self.read_str(REG_VERSION, 2),
            "date": self.read_str(REG_DATE, 3),
            "manufacturer": self.read_str(REG_MANUF, 8),
            "product": self.read_str(REG_PRODUCT, 8),
            "serial": self.read_str(REG_SERIAL, 7),
            "vidpid": self.read(REG_VIDPID),
            "flags": flags,
            "single_dac": bool(flags & 0x0200),
            "magic_ok": self.read(REG_MAGIC) == MAGIC,
        }

    # ---- EQ ----
    def eq_read(self, bank="dac"):
        base, en = (EQ_DAC_BASE, EQ_DAC_EN) if bank == "dac" else (EQ_ADC_BASE, EQ_ADC_EN)
        bands = []
        for i in range(EQ_BANDS):
            a = self.read(base + i * 2)
            b = self.read(base + i * 2 + 1)
            freq = a >> 16
            gain = struct.unpack("<h", struct.pack("<H", a & 0xFFFF))[0]
            q = b & 0xFFFF
            typ = (b >> 16) & 0x7
            bands.append({"freq": freq, "gain": gain / 10.0,
                          "q": q / 1000.0, "type": TYPE_NAMES.get(typ, str(typ))})
        return {"enabled": bool(self.read(en) & 1), "bands": bands}

    def eq_write_band(self, bank, index, freq=None, gain=None, q=None, typ=None):
        base = EQ_DAC_BASE if bank == "dac" else EQ_ADC_BASE
        a_addr = base + index * 2
        cur = self.eq_read(bank)["bands"][index]
        freq = cur["freq"] if freq is None else int(freq)
        gain = cur["gain"] if gain is None else float(gain)
        q = cur["q"] if q is None else float(q)
        typ = cur["type"] if typ is None else typ
        a = ((freq & 0xFFFF) << 16) | (int(round(gain * 10)) & 0xFFFF)
        b = (TYPE_IDS[typ] << 16) | (int(round(q * 1000)) & 0xFFFF)
        self.write(a_addr, a)
        self.write(a_addr + 1, b)

    def eq_enable(self, bank, on):
        en = EQ_DAC_EN if bank == "dac" else EQ_ADC_EN
        cur = self.read(en)
        self.write(en, (cur | 1) if on else (cur & ~1))


# ═══════════════════════════════════════════════════════════════
#  biquad math (RBJ cookbook) — pure python
# ═══════════════════════════════════════════════════════════════
def biquad(freq, gain_db, q, typ, fs=48000):
    f = max(1.0, min(freq, fs / 2 * 0.99))
    A = 10 ** (gain_db / 40.0)
    w0 = 2 * math.pi * f / fs
    cw, sw = math.cos(w0), math.sin(w0)
    alpha = sw / (2 * q) if q > 0 else 0.0
    if typ in ("PK", 0):
        b0, b1, b2 = 1 + alpha * A, -2 * cw, 1 - alpha * A
        a0, a1, a2 = 1 + alpha / A, -2 * cw, 1 - alpha / A
    elif typ in ("LPF", 1):
        b0, b1, b2 = (1 - cw) / 2, 1 - cw, (1 - cw) / 2
        a0, a1, a2 = 1 + alpha, -2 * cw, 1 - alpha
    elif typ in ("HPF", 2):
        b0, b1, b2 = (1 + cw) / 2, -(1 + cw), (1 + cw) / 2
        a0, a1, a2 = 1 + alpha, -2 * cw, 1 - alpha
    else:  # shelves
        s = 1.0 if typ in ("LSH", 3) else -1.0
        sq = math.sqrt(A) / q if q > 0 else math.sqrt(A)
        alpha_s = sw / 2 * math.sqrt((A + 1 / A) * (1 / s - 1) + 2) if False else sw / 2 * sq
        two = 2 * math.sqrt(A) * alpha_s
        if typ in ("LSH", 3):
            b0 = A * ((A + 1) - (A - 1) * cw + two)
            b1 = 2 * A * ((A - 1) - (A + 1) * cw)
            b2 = A * ((A + 1) - (A - 1) * cw - two)
            a0 = (A + 1) + (A - 1) * cw + two
            a1 = -2 * ((A - 1) + (A + 1) * cw)
            a2 = (A + 1) + (A - 1) * cw - two
        else:
            b0 = A * ((A + 1) + (A - 1) * cw + two)
            b1 = -2 * A * ((A - 1) + (A + 1) * cw)
            b2 = A * ((A + 1) + (A - 1) * cw - two)
            a0 = (A + 1) - (A - 1) * cw + two
            a1 = 2 * ((A - 1) - (A + 1) * cw)
            a2 = (A + 1) - (A - 1) * cw - two
    return (b0 / a0, b1 / a0, b2 / a0, a1 / a0, a2 / a0)


def response(bands, freqs, fs=48000):
    out = []
    for f in freqs:
        z = complex(math.cos(2 * math.pi * f / fs), -math.sin(2 * math.pi * f / fs))
        mag = 1.0
        for b in bands:
            b0, b1, b2, a1, a2 = biquad(b["freq"], b["gain"], b["q"], b["type"], fs)
            num = b0 + b1 / z + b2 / (z * z)
            den = 1 + a1 / z + a2 / (z * z)
            mag *= abs(num / den)
        out.append(20 * math.log10(max(mag, 1e-9)))
    return out


# ═══════════════════════════════════════════════════════════════
#  SVG plotting
# ═══════════════════════════════════════════════════════════════
def plot_svg(bands, path, fs=48000, title="DAC equaliser — measured register state"):
    freqs = [20 * (1000 ** (i / 400)) for i in range(401)]  # 20 Hz .. 20 kHz
    db = response(bands, freqs, fs)
    W, H, L, R, T, B = 900, 420, 64, 24, 44, 52
    lo, hi = -12, 12
    xmax, xmin = math.log10(20000), math.log10(20)
    ymax, ymin = math.log10(1000), math.log10(10)

    def X(f):
        return L + (math.log10(f) - xmin) / (xmax - xmin) * (W - L - R)

    def Y(d):
        d = max(lo, min(hi, d))
        return T + (hi - d) / (hi - lo) * (H - T - B)

    parts = [f'<svg xmlns="http://www.w3.org/2000/svg" width="{W}" height="{H}" '
             f'viewBox="0 0 {W} {H}" font-family="DejaVu Sans, sans-serif">',
             f'<rect width="{W}" height="{H}" fill="#0e1116"/>',
             f'<text x="{L}" y="26" fill="#e8eaed" font-size="15">{title}</text>']
    for f in (20, 50, 100, 200, 500, 1000, 2000, 5000, 10000, 20000):
        x = X(f)
        parts.append(f'<line x1="{x:.1f}" y1="{T}" x2="{x:.1f}" y2="{H-B}" stroke="#232a33"/>')
        lab = f"{f//1000}k" if f >= 1000 else str(f)
        parts.append(f'<text x="{x:.1f}" y="{H-B+18}" fill="#8b949e" font-size="11" '
                     f'text-anchor="middle">{lab}</text>')
    for d in range(lo, hi + 1, 3):
        y = Y(d)
        parts.append(f'<line x1="{L}" y1="{y:.1f}" x2="{W-R}" y2="{y:.1f}" '
                     f'stroke="{"#2c3542" if d == 0 else "#1b222b"}"/>')
        parts.append(f'<text x="{L-8}" y="{y+4:.1f}" fill="#8b949e" font-size="11" '
                     f'text-anchor="end">{d:+d} dB</text>')
    xy = [(X(f), Y(v)) for f, v in zip(freqs, db)]
    for (x1, y1), (x2, y2) in zip(xy, xy[1:]):
        parts.append(f'<line x1="{x1:.1f}" y1="{y1:.1f}" x2="{x2:.1f}" y2="{y2:.1f}" '
                     f'stroke="#4ec9b0" stroke-width="2"/>')
    for b in bands:
        if b["type"] in ("PK", 0) and abs(b["gain"]) > 0.05:
            x, y = X(b["freq"]), Y(b["gain"])
            parts.append(f'<circle cx="{x:.1f}" cy="{y:.1f}" r="4" fill="#f2b84b"/>')
            parts.append(f'<text x="{x:.1f}" y="{y-10:.1f}" fill="#f2b84b" font-size="10" '
                         f'text-anchor="middle">{b["freq"]} Hz {b["gain"]:+.1f} dB Q{b["q"]:.2f}</text>')
    parts.append(f'<text x="{W-R}" y="{H-10}" fill="#4a5563" font-size="10" text-anchor="end">'
                 f'fs={fs} Hz · {len(bands)} biquads</text>')
    parts.append("</svg>")
    open(path, "w").write("\n".join(parts))
    return db, freqs


# ═══════════════════════════════════════════════════════════════
#  CLI
# ═══════════════════════════════════════════════════════════════
def fmt_bands(eq, bank):
    print(f"{bank.upper()} EQ: {'ON' if eq['enabled'] else 'OFF'}")
    print(f"{'#':<2}{'type':<5}{'freq(Hz)':>10}{'gain(dB)':>10}{'Q':>8}")
    for i, b in enumerate(eq["bands"]):
        print(f"{i:<2}{b['type']:<5}{b['freq']:>10}{b['gain']:>10.1f}{b['q']:>8.3f}")


def main():
    ap = argparse.ArgumentParser(description="KTMicro USB DSP control (TANCHJIM BUNNY DSP)")
    ap.add_argument("--pid", type=lambda x: int(x, 0), default=None)
    sub = ap.add_subparsers(dest="cmd", required=True)

    sub.add_parser("info")
    p = sub.add_parser("dump")
    p.add_argument("-o", "--out", default="regdump.json")

    p = sub.add_parser("eq")
    p.add_argument("--bank", choices=["dac", "adc"], default="dac")
    p.add_argument("--json", action="store_true")
    p.add_argument("--enable", action="store_true")
    p.add_argument("--disable", action="store_true")
    p.add_argument("--set", action="append", default=[],
                   help="i:freq,gain,q,type  e.g. 0:30,4.0,0.7,PK")
    p.add_argument("--plot", default=None, metavar="OUT.svg")

    sub.add_parser("vol")
    sub.add_parser("drc")
    p = sub.add_parser("raw")
    p.add_argument("addr", type=lambda x: int(x, 0))
    p.add_argument("value", nargs="?", type=lambda x: int(x, 0))
    p = sub.add_parser("save", help="commit RAM -> flash (0x53), device reboots")
    p.add_argument("--yes", action="store_true")

    args = ap.parse_args()

    with KTDevice(pid=args.pid) as dev:
        if args.cmd == "info":
            info = dev.info()
            print(f"device   : {dev.path}")
            print(f"vendor   : {info['manufacturer']}")
            print(f"product  : {info['product']}")
            print(f"serial   : {info['serial']}")
            print(f"firmware : {info['version']}  ({info['date']})")
            vp = info["vidpid"]
            print(f"vid:pid  : {vp & 0xFFFF:04X}:{vp >> 16:04X}")
            print(f"flags    : 0x{info['flags']:08X}  "
                  f"({'single-DAC' if info['single_dac'] else 'stereo-DAC'})")
            print(f"magic    : {'OK' if info['magic_ok'] else 'MISSING'}")

        elif args.cmd == "dump":
            regs = {f"0x{a:02X}": dev.read(a) for a in range(256)}
            blob = {"device": dev.info(), "regs": regs, "t": time.strftime("%F %T")}
            json.dump(blob, open(args.out, "w"), indent=1)
            print(f"wrote {args.out}")

        elif args.cmd == "eq":
            if args.enable or args.disable:
                dev.eq_enable(args.bank, bool(args.enable))
            for spec in args.set:
                idx, vals = spec.split(":", 1)
                f, g, q, t = [x.strip() for x in vals.split(",")]
                dev.eq_write_band(args.bank, int(idx), float(f), float(g), float(q), t.upper())
            eq = dev.eq_read(args.bank)
            if args.json:
                print(json.dumps(eq, indent=1))
            else:
                fmt_bands(eq, args.bank)
            if args.plot:
                bands = [b for b in eq["bands"] if b["freq"] > 0]
                db, _ = plot_svg(bands, args.plot)
                print(f"plot  -> {args.plot}   (peak {max(db):+.1f} dB / min {min(db):+.1f} dB)")

        elif args.cmd == "vol":
            print(f"A_ADC PGA (0x3A): idx {dev.read(PGA_ADC)}")
            print(f"A_DAC PGA (0x3B): idx {dev.read(PGA_DAC)}")
            print(f"DIG_ADC  (0x65): {struct.unpack('b', bytes([dev.read(DIG_ADC) & 0xFF]))[0] / 2:+.1f} dB")
            d = dev.read(DIG_DAC)
            print(f"DIG_DAC  (0x66): L {struct.unpack('b', bytes([d & 0xFF]))[0] / 2:+.1f} dB  "
                  f"R {struct.unpack('b', bytes([(d >> 8) & 0xFF]))[0] / 2:+.1f} dB")

        elif args.cmd == "drc":
            ng, ng2, ng3 = dev.read(NG_BASE), dev.read(NG_BASE + 1), dev.read(NG_BASE + 2)
            lim, lim2 = dev.read(LIM_BASE), dev.read(LIM_BASE + 1)
            print(f"noise gate 0x71=0x{ng:08X} 0x72=0x{ng2:08X} 0x73=0x{ng3:08X}")
            print(f"  enable={bool(ng >> 31)}  gate_vol={(ng >> 16 & 0xFF) - 256} dB  "
                  f"th_hi={(ng >> 8 & 0xFF) - 256} dB  th_lo={(ng & 0xFF) - 256} dB")
            print(f"limiter    0x78=0x{lim:08X} 0x79=0x{lim2:08X}")
            print(f"  enable={bool(lim >> 31)}  soft={bool((lim >> 30) & 1)}  "
                  f"th={(lim & 0xFF) - 256} dB")

        elif args.cmd == "raw":
            if args.value is None:
                print(f"0x{args.addr:02X} = 0x{dev.read(args.addr):08X}")
            else:
                st = dev.write(args.addr, args.value)
                print(f"write 0x{args.addr:02X} <- 0x{args.value:08X}  ack=0x{st:02X}  "
                      f"readback=0x{dev.read(args.addr):08X}")

        elif args.cmd == "save":
            if not args.yes:
                print("refusing without --yes: 0x53 rewrites the config sector and reboots")
                return 2
            print("save:", dev.save_flash())


if __name__ == "__main__":
    sys.exit(main())
