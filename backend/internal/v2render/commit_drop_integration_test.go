package v2render

import (
	"encoding/binary"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
)

// Plain PostgreSQL test transport drops only a confirmed COMMIT command tag.
// Authentication/query bytes are never logged or persisted.
type commitDropConn struct {
	net.Conn
	readMu  sync.Mutex
	armed   atomic.Bool
	dropped *atomic.Bool
	pending []byte
}

func (c *commitDropConn) Write(p []byte) (int, error) {
	if len(p) >= 5 && p[0] == 'Q' && int(binary.BigEndian.Uint32(p[1:5])) == len(p)-1 && strings.EqualFold(strings.TrimSpace(strings.TrimSuffix(string(p[5:]), "\x00")), "commit") {
		c.armed.Store(true)
	}
	return c.Conn.Write(p)
}
func (c *commitDropConn) Read(p []byte) (int, error) {
	c.readMu.Lock()
	defer c.readMu.Unlock()
	if len(p) == 0 {
		return 0, nil
	}
	if len(c.pending) == 0 {
		header := make([]byte, 5)
		if _, err := io.ReadFull(c.Conn, header); err != nil {
			return 0, err
		}
		size := int(binary.BigEndian.Uint32(header[1:]))
		if size < 4 || size > 64*1024*1024 {
			return 0, io.ErrUnexpectedEOF
		}
		body := make([]byte, size-4)
		if _, err := io.ReadFull(c.Conn, body); err != nil {
			return 0, err
		}
		if c.armed.Load() && header[0] == 'C' && string(body) == "COMMIT\x00" {
			c.dropped.Store(true)
			c.Conn.Close()
			return 0, io.ErrUnexpectedEOF
		}
		c.pending = append(header, body...)
	}
	n := copy(p, c.pending)
	c.pending = c.pending[n:]
	return n, nil
}
