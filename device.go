package main

// KTMicro (KT0211L / Helios) USB DSP protocol.
//
// Frame format (11 bytes, report 0x4B):
//   read  4B <addr LE32> 52 00 00 00 00 00  -> 4B <addr> 52 00 <value LE32>
//   write 4B <addr LE32> 57 00 <value LE32> -> 4B <addr> 57 00 <status LE32>
//   save  4B 00 00 00 00 53 00 00 00 00 00 -> 4B ... 53 ...   (writes flash, reboots)
//
// Only 4-byte aligned 0x08 reads are safe on this firmware; we never use 0x08.
// Platform specific bits (hidraw + select) live in hid_linux.go / hid_other.go.

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"
)

const (
	ktVID    = 0x31B2
	reportID = 0x4B
)

var (
	errNotConnected = errors.New("耳机未连接")
	errTimeout      = errors.New("设备响应超时")
	errUnsupported  = errors.New("当前系统不支持：需要 Linux 的 /dev/hidraw（见 README「平台支持」）")
)

// Register addresses (Helios layout).
const (
	RegFlags   = 0x01
	RegVersion = 0x04
	RegDate    = 0x08
	RegManuf   = 0x40
	RegProduct = 0x48
	RegSerial  = 0x50
	RegVIDPID  = 0x5B
	RegMagic   = 0xE1
	RegDigDAC  = 0x66
)

var bankMap = map[string]struct {
	Enable uint8
	Base   uint8
}{
	"dac": {0x24, 0x26},
	"adc": {0x18, 0x1A},
}

const numBands = 5

var typeID = map[string]uint8{"PK": 0, "LPF": 1, "HPF": 2, "LSH": 3, "HSH": 4}
var typeName = map[uint8]string{0: "PK", 1: "LPF", 2: "HPF", 3: "LSH", 4: "HSH"}

// Band is one parametric EQ section.
type Band struct {
	Freq int     `json:"freq"`
	Gain float64 `json:"gain"`
	Q    float64 `json:"q"`
	Type string  `json:"type"`
}

// EQ is one filter bank (dac = headphone path, adc = microphone path).
type EQ struct {
	Bank    string `json:"bank"`
	Enabled bool   `json:"enabled"`
	Bands   []Band `json:"bands"`
}

// Info is the device identity block.
type Info struct {
	Path         string `json:"path"`
	Manufacturer string `json:"manufacturer"`
	Product      string `json:"product"`
	Serial       string `json:"serial"`
	Version      string `json:"version"`
	Date         string `json:"date"`
	VIDPID       string `json:"vidpid"`
	SingleDAC    bool   `json:"single_dac"`
	Flags        uint32 `json:"flags"`
	Magic        bool   `json:"magic"`
}

// Device serialises every access to the HID node.
type Device struct {
	mu      sync.Mutex
	h       *hidConn
	path    string
	lastErr string
}

// LastError is the most recent failure, shown in the web UI.
func (d *Device) LastError() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.lastErr
}

func (d *Device) fail(err error) error {
	if err != nil {
		d.lastErr = err.Error()
	}
	return err
}

func (d *Device) find() (string, error) {
	paths, err := hidPaths()
	if err != nil {
		return "", err
	}
	if len(paths) == 0 {
		return "", errNotConnected
	}
	return paths[0], nil
}

// Connected reports whether a usable handle is open.
func (d *Device) Connected() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.h != nil
}

func (d *Device) ensureLocked() error {
	if d.h != nil {
		return nil
	}
	path, err := d.find()
	if err != nil {
		return d.fail(err)
	}
	c, err := openHID(path)
	if err != nil {
		d.lastErr = "无法打开 " + path + "（检查 udev 规则）"
		return errNotConnected
	}
	d.h, d.path, d.lastErr = c, path, ""
	return nil
}

func (d *Device) dropLocked() {
	if d.h != nil {
		d.h.Close()
		d.h = nil
	}
}

// Reconnect polls until the dongle re-enumerates (it reboots after a save).
func (d *Device) Reconnect(wait time.Duration) error {
	deadline := time.Now().Add(wait)
	for {
		time.Sleep(250 * time.Millisecond)
		d.mu.Lock()
		d.dropLocked()
		err := d.ensureLocked()
		d.mu.Unlock()
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return err
		}
	}
}

// xferLocked sends one frame and returns the matching response.
func (d *Device) xferLocked(req []byte, cmd byte, addr uint32, hasAddr bool) ([]byte, error) {
	if d.h == nil {
		return nil, errNotConnected
	}
	buf := make([]byte, 64)
	// discard stale input reports
	for {
		n, err := d.h.Read(buf, 0)
		if err != nil || n == 0 {
			break
		}
	}
	if err := d.h.Write(req); err != nil {
		d.dropLocked()
		return nil, d.fail(fmt.Errorf("写入失败: %w", err))
	}
	deadline := time.Now().Add(400 * time.Millisecond)
	for {
		remain := time.Until(deadline)
		if remain <= 0 {
			return nil, d.fail(errTimeout)
		}
		n, err := d.h.Read(buf, remain)
		if err != nil {
			if errors.Is(err, errTimeout) {
				return nil, d.fail(errTimeout)
			}
			d.dropLocked()
			return nil, d.fail(err)
		}
		resp := buf[:n]
		if len(resp) < 11 || resp[0] != reportID || resp[5] != cmd {
			continue
		}
		if hasAddr && u32(resp[1:5]) != addr {
			continue
		}
		return resp, nil
	}
}

// ---- lock-free primitives (caller holds d.mu) ----

func (d *Device) readLocked(addr uint8) (uint32, error) {
	req := []byte{reportID, addr, 0, 0, 0, 0x52, 0, 0, 0, 0, 0}
	resp, err := d.xferLocked(req, 0x52, uint32(addr), true)
	if err != nil {
		return 0, err
	}
	return u32(resp[7:11]), nil
}

func (d *Device) writeLocked(addr uint8, value uint32) error {
	req := []byte{reportID, addr, 0, 0, 0, 0x57, 0,
		byte(value), byte(value >> 8), byte(value >> 16), byte(value >> 24)}
	resp, err := d.xferLocked(req, 0x57, uint32(addr), true)
	if err != nil {
		return err
	}
	if st := resp[7]; st != 0x03 && st != 0x4F {
		return fmt.Errorf("寄存器 0x%02X 写入被拒绝 (0x%02X)", addr, st)
	}
	return nil
}

func (d *Device) readStringLocked(addr uint8, words int) (string, error) {
	var sb strings.Builder
	for i := 0; i < words; i++ {
		v, err := d.readLocked(addr + uint8(i))
		if err != nil {
			return "", err
		}
		sb.Write([]byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24)})
	}
	s := sb.String()
	if i := strings.IndexByte(s, 0); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s), nil
}

func u32(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}

func clampF(f, lo, hi float64) float64 {
	if f < lo {
		return lo
	}
	if f > hi {
		return hi
	}
	return f
}

// encodeBand packs a Band into the two device registers.
//
//	A = [freq:16][gain×10:16 signed]
//	B = [type:3][reserved:13][Q×1000:16]
func encodeBand(b Band) (a, bb uint32, err error) {
	tid, ok := typeID[strings.ToUpper(strings.TrimSpace(b.Type))]
	if !ok {
		return 0, 0, fmt.Errorf("未知滤波器类型 %q", b.Type)
	}
	f := int(clampF(float64(b.Freq), 20, 20000))
	g := int(math.Round(clampF(b.Gain, -24, 24) * 10))
	q := int(math.Round(clampF(b.Q, 0.05, 20) * 1000))
	a = uint32(f&0xFFFF)<<16 | uint32(uint16(int16(g)))
	bb = uint32(tid&7)<<16 | uint32(q&0xFFFF)
	return a, bb, nil
}

// decodeBand unpacks the two registers back into a Band.
func decodeBand(a, b uint32) Band {
	return Band{
		Freq: int(a >> 16),
		Gain: float64(int16(uint16(a))) / 10,
		Q:    float64(b&0xFFFF) / 1000,
		Type: typeName[uint8((b>>16)&7)],
	}
}

// ---- public API ----

// Read returns one 32-bit DSP register.
func (d *Device) Read(addr uint8) (uint32, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.ensureLocked(); err != nil {
		return 0, err
	}
	return d.readLocked(addr)
}

// Write stores one 32-bit DSP register (RAM only).
func (d *Device) Write(addr uint8, value uint32) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.ensureLocked(); err != nil {
		return err
	}
	return d.writeLocked(addr, value)
}

// DeviceInfo reads the identity block.
func (d *Device) DeviceInfo() (Info, error) {
	var info Info
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.ensureLocked(); err != nil {
		return info, err
	}
	var err error
	if info.Product, err = d.readStringLocked(RegProduct, 8); err != nil {
		return info, err
	}
	info.Manufacturer, _ = d.readStringLocked(RegManuf, 8)
	info.Serial, _ = d.readStringLocked(RegSerial, 7)
	if v1, e := d.readStringLocked(RegVersion, 1); e == nil {
		if v2, e2 := d.readStringLocked(RegVersion+1, 1); e2 == nil {
			info.Version = v1 + v2
		}
	}
	info.Date, _ = d.readStringLocked(RegDate, 3)
	flags, err := d.readLocked(RegFlags)
	if err != nil {
		return info, err
	}
	info.Flags = flags
	info.SingleDAC = flags&0x0200 != 0
	if vidpid, e := d.readLocked(RegVIDPID); e == nil {
		info.VIDPID = fmt.Sprintf("%04X:%04X", vidpid&0xFFFF, vidpid>>16)
	}
	if magic, e := d.readLocked(RegMagic); e == nil {
		info.Magic = magic == 0x12345678
	}
	info.Path = d.path
	return info, nil
}

// ReadEQ decodes one filter bank.
func (d *Device) ReadEQ(bank string) (EQ, error) {
	cfg, ok := bankMap[bank]
	if !ok {
		return EQ{}, fmt.Errorf("未知的通道 %q", bank)
	}
	eq := EQ{Bank: bank, Bands: make([]Band, 0, numBands)}
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.ensureLocked(); err != nil {
		return eq, err
	}
	en, err := d.readLocked(cfg.Enable)
	if err != nil {
		return eq, err
	}
	eq.Enabled = en&1 != 0
	for i := 0; i < numBands; i++ {
		a, err := d.readLocked(cfg.Base + uint8(i*2))
		if err != nil {
			return eq, err
		}
		b, err := d.readLocked(cfg.Base + uint8(i*2) + 1)
		if err != nil {
			return eq, err
		}
		eq.Bands = append(eq.Bands, decodeBand(a, b))
	}
	return eq, nil
}

// WriteEQ encodes one filter bank (RAM; call Save to persist).
func (d *Device) WriteEQ(eq EQ) error {
	cfg, ok := bankMap[eq.Bank]
	if !ok {
		return fmt.Errorf("未知的通道 %q", eq.Bank)
	}
	if len(eq.Bands) != numBands {
		return fmt.Errorf("需要 %d 段，收到 %d 段", numBands, len(eq.Bands))
	}
	encoded := make([][2]uint32, 0, numBands)
	for _, b := range eq.Bands {
		a, bb, err := encodeBand(b)
		if err != nil {
			return err
		}
		encoded = append(encoded, [2]uint32{a, bb})
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.ensureLocked(); err != nil {
		return err
	}
	for i, regs := range encoded {
		if err := d.writeLocked(cfg.Base+uint8(i*2), regs[0]); err != nil {
			return err
		}
		if err := d.writeLocked(cfg.Base+uint8(i*2)+1, regs[1]); err != nil {
			return err
		}
	}
	en, err := d.readLocked(cfg.Enable)
	if err != nil {
		return err
	}
	if eq.Enabled {
		en |= 1
	} else {
		en &^= 1
	}
	return d.writeLocked(cfg.Enable, en)
}

// Preamp is the digital DAC gain in dB (0.5 dB steps).
type Preamp struct {
	L float64 `json:"l"`
	R float64 `json:"r"`
}

func (d *Device) ReadPreamp() (Preamp, error) {
	var p Preamp
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.ensureLocked(); err != nil {
		return p, err
	}
	v, err := d.readLocked(RegDigDAC)
	if err != nil {
		return p, err
	}
	p.L = float64(int8(byte(v))) / 2
	p.R = float64(int8(byte(v>>8))) / 2
	return p, nil
}

func (d *Device) WritePreamp(p Preamp) error {
	enc := func(db float64) byte { return byte(int8(math.Round(clampF(db, -24, 12) * 2))) }
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.ensureLocked(); err != nil {
		return err
	}
	v, err := d.readLocked(RegDigDAC)
	if err != nil {
		return err
	}
	v = (v & 0xFFFF0000) | uint32(enc(p.L)) | uint32(enc(p.R))<<8
	return d.writeLocked(RegDigDAC, v)
}

// Save commits live RAM to flash (0x53) and waits for the device to come back.
func (d *Device) Save() error {
	d.mu.Lock()
	if err := d.ensureLocked(); err != nil {
		d.mu.Unlock()
		return err
	}
	_, err := d.xferLocked([]byte{reportID, 0, 0, 0, 0, 0x53, 0, 0, 0, 0, 0}, 0x53, 0, false)
	d.dropLocked()
	d.mu.Unlock()
	if err != nil {
		return err
	}
	return d.Reconnect(15 * time.Second)
}

// DumpAll returns all 256 registers keyed by hex address.
func (d *Device) DumpAll() (map[string]uint32, error) {
	out := make(map[string]uint32, 256)
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.ensureLocked(); err != nil {
		return out, err
	}
	for a := 0; a < 256; a++ {
		v, err := d.readLocked(uint8(a))
		if err != nil {
			return out, err
		}
		out[fmt.Sprintf("0x%02X", a)] = v
	}
	return out, nil
}
