package main

// Ragnarok Online wire protocol for rAthena PACKETVER 20210107 (kRO RagexeRE): little-endian packets with a
// u16 opcode; fixed length per opcode, or -1 = variable (u16 length at offset 2). The length table is the
// client's own (recvpackets.txt, same table OpenKore uses for this server).

import (
	"bufio"
	_ "embed"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

//go:embed recvpackets.txt
var recvPacketsTxt string

var pktLen = map[uint16]int{}

func init() {
	for _, line := range strings.Split(recvPacketsTxt, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 || len(f[0]) != 4 {
			continue
		}
		op, err1 := strconv.ParseUint(f[0], 16, 16)
		n, err2 := strconv.Atoi(f[1])
		if err1 == nil && err2 == nil {
			pktLen[uint16(op)] = n
		}
	}
}

var le = binary.LittleEndian

// Conn wraps a TCP connection with RO packet framing.
type Conn struct {
	c net.Conn
	r *bufio.Reader
}

func dial(addr string) (*Conn, error) {
	c, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return nil, err
	}
	if tc, ok := c.(*net.TCPConn); ok {
		tc.SetNoDelay(true)
	}
	return &Conn{c: c, r: bufio.NewReaderSize(c, 16384)}, nil
}

func (c *Conn) Close() { c.c.Close() }

func (c *Conn) Send(p []byte) error {
	c.c.SetWriteDeadline(time.Now().Add(10 * time.Second))
	_, err := c.c.Write(p)
	return err
}

// ReadRaw reads exactly n bytes (used for the char server's 4-byte account id greeting).
func (c *Conn) ReadRaw(n int, timeout time.Duration) ([]byte, error) {
	c.c.SetReadDeadline(time.Now().Add(timeout))
	b := make([]byte, n)
	_, err := io.ReadFull(c.r, b)
	return b, err
}

var errUnknownPacket = errors.New("unknown packet")

// Read returns the next complete packet (opcode included).
func (c *Conn) Read(timeout time.Duration) ([]byte, error) {
	c.c.SetReadDeadline(time.Now().Add(timeout))
	hdr, err := c.r.Peek(2)
	if err != nil {
		return nil, err
	}
	op := le.Uint16(hdr)
	n, ok := pktLen[op]
	if !ok {
		return nil, fmt.Errorf("%w %04x", errUnknownPacket, op)
	}
	if n == -1 {
		h, err := c.r.Peek(4)
		if err != nil {
			return nil, err
		}
		n = int(le.Uint16(h[2:]))
	}
	if n < 2 {
		return nil, fmt.Errorf("bad length %d for %04x", n, op)
	}
	b := make([]byte, n)
	_, err = io.ReadFull(c.r, b)
	return b, err
}

// packet builder
type pw struct{ b []byte }

func newPkt(op uint16) *pw       { p := &pw{}; p.u16(op); return p }
func (p *pw) u8(v byte) *pw      { p.b = append(p.b, v); return p }
func (p *pw) u16(v uint16) *pw   { p.b = le.AppendUint16(p.b, v); return p }
func (p *pw) u32(v uint32) *pw   { p.b = le.AppendUint32(p.b, v); return p }
func (p *pw) bytes(v []byte) *pw { p.b = append(p.b, v...); return p }
func (p *pw) str(s string, n int) *pw {
	b := make([]byte, n)
	copy(b, s)
	p.b = append(p.b, b...)
	return p
}
func (p *pw) varlen() []byte { le.PutUint16(p.b[2:], uint16(len(p.b))); return p.b }

func cstr(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}

// 3-byte position (x, y, dir)
func encodePos(x, y, dir int) []byte {
	return []byte{byte(x >> 2), byte((x&3)<<6 | (y>>4)&0x3f), byte((y&0xf)<<4 | dir&0xf)}
}

func decodePos(b []byte) (x, y, dir int) {
	x = int(b[0])<<2 | int(b[1])>>6
	y = int(b[1]&0x3f)<<4 | int(b[2])>>4
	dir = int(b[2] & 0xf)
	return
}

// 6-byte move data (from x0,y0 to x1,y1)
func decodeMove(b []byte) (x0, y0, x1, y1 int) {
	x0 = int(b[0])<<2 | int(b[1])>>6
	y0 = int(b[1]&0x3f)<<4 | int(b[2])>>4
	x1 = int(b[2]&0xf)<<6 | int(b[3])>>2
	y1 = int(b[3]&3)<<8 | int(b[4])
	return
}

func ip4(v uint32) string {
	return fmt.Sprintf("%d.%d.%d.%d", byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
}
