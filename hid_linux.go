//go:build linux

package main

// Linux HID transport: the KT dongle exposes a hidraw node with 11-byte
// interrupt reports. We keep it O_NONBLOCK and wait with select(2) so every
// call has a hard timeout instead of hanging a HTTP handler forever.

import (
	"errors"
	"os"
	"strings"
	"syscall"
	"time"
)

type hidConn struct {
	f    *os.File
	path string
}

func openHID(path string) (*hidConn, error) {
	f, err := os.OpenFile(path, os.O_RDWR|syscall.O_NONBLOCK, 0o600)
	if err != nil {
		return nil, err
	}
	return &hidConn{f: f, path: path}, nil
}

func (c *hidConn) Path() string { return c.path }
func (c *hidConn) Close()       { _ = c.f.Close() }

func (c *hidConn) Write(p []byte) error {
	_, err := c.f.Write(p)
	return err
}

// Read waits up to timeout for one input report.
func (c *hidConn) Read(buf []byte, timeout time.Duration) (int, error) {
	if err := waitReadable(c.f.Fd(), timeout); err != nil {
		return 0, err
	}
	n, err := c.f.Read(buf)
	if err != nil {
		if errors.Is(err, syscall.EAGAIN) {
			return 0, errTimeout
		}
		return 0, err
	}
	return n, nil
}

func waitReadable(fd uintptr, timeout time.Duration) error {
	// Go's runtime uses signals (SIGURG) for preemption, which can interrupt
	// select(2) with EINTR at any time — retry instead of failing the request.
	deadline := time.Now().Add(timeout)
	for {
		remain := time.Until(deadline)
		if remain < 0 {
			return errTimeout
		}
		var rfds syscall.FdSet
		idx := fd / 64
		if idx >= uintptr(len(rfds.Bits)) {
			return errors.New("文件描述符超出 select 范围")
		}
		rfds.Bits[idx] |= 1 << (fd % 64)
		tv := syscall.NsecToTimeval(remain.Nanoseconds())
		n, err := syscall.Select(int(fd)+1, &rfds, nil, nil, &tv)
		if err == syscall.EINTR {
			continue
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return errTimeout
		}
		return nil
	}
}

// hidPaths lists hidraw nodes belonging to the KT vendor id.
func hidPaths() ([]string, error) {
	entries, err := os.ReadDir("/sys/class/hidraw")
	if err != nil {
		return nil, errUnsupported
	}
	var out []string
	for _, e := range entries {
		ue := "/sys/class/hidraw/" + e.Name() + "/device/uevent"
		b, err := os.ReadFile(ue)
		if err != nil {
			continue
		}
		if strings.Contains(strings.ToUpper(string(b)), "31B2") {
			out = append(out, "/dev/"+e.Name())
		}
	}
	return out, nil
}
