//go:build !linux

package main

// Non-Linux stub: the binary still builds and the web UI still loads, it just
// reports that the platform is unsupported instead of failing at compile time.
// Porting means implementing these four functions on the target OS (Windows:
// SetupAPI/HidD_*, macOS: IOHIDManager).

import (
	"time"
)

type hidConn struct {
	path string
}

func openHID(path string) (*hidConn, error) { return nil, errUnsupported }

func (c *hidConn) Path() string                                  { return c.path }
func (c *hidConn) Close()                                        {}
func (c *hidConn) Write(p []byte) error                          { return errUnsupported }
func (c *hidConn) Read(buf []byte, t time.Duration) (int, error) { return 0, errUnsupported }

func hidPaths() ([]string, error) { return nil, errUnsupported }
