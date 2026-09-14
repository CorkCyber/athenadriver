// SPDX-License-Identifier: MIT

package statsd

import (
	"net"
	"strings"
	"testing"
	"time"

	drv "github.com/CorkCyber/athenadriver/v2/go"
	"github.com/cactus/go-statsd-client/v5/statsd"
)

func newTestStatter(t *testing.T) statsd.Statter {
	t.Helper()
	s, err := statsd.NewClient("127.0.0.1:0", "test")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// udpSink binds an ephemeral UDP listener and returns a statsd Statter
// pointing at it plus a `read` closure that drains up to `want` lines with
// a 2s deadline. Shared by every live-fire test.
func udpSink(t *testing.T, prefix string) (statsd.Statter, func(want int) []string) {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ListenPacket: %v", err)
	}
	t.Cleanup(func() { _ = pc.Close() })

	statter, err := statsd.NewClient(pc.LocalAddr().String(), prefix)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(func() { _ = statter.Close() })

	read := func(want int) []string {
		if err := pc.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
			t.Fatalf("SetReadDeadline: %v", err)
		}
		var lines []string
		for len(lines) < want {
			buf := make([]byte, 1500)
			n, _, err := pc.ReadFrom(buf)
			if err != nil {
				break
			}
			for _, l := range strings.Split(strings.TrimRight(string(buf[:n]), "\n"), "\n") {
				if l != "" {
					lines = append(lines, l)
				}
			}
		}
		return lines
	}
	return statter, read
}

// readMaybe drains packets until a short deadline, returning whatever
// arrives — used to assert non-emission.
func readMaybe(t *testing.T, pc net.PacketConn, budget time.Duration) []string {
	t.Helper()
	if err := pc.SetReadDeadline(time.Now().Add(budget)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	var lines []string
	for {
		buf := make([]byte, 1500)
		n, _, err := pc.ReadFrom(buf)
		if err != nil {
			return lines
		}
		for _, l := range strings.Split(strings.TrimRight(string(buf[:n]), "\n"), "\n") {
			if l != "" {
				lines = append(lines, l)
			}
		}
	}
}

func TestAdapterSatisfiesScope(t *testing.T) {
	s := newTestStatter(t)
	var _ drv.Scope = New(s)
	var _ drv.Scope = NewWithRate(s, 0.5)
}

// TestAdapterLiveFire asserts counter + timing serialize onto the wire in
// the expected format.
func TestAdapterLiveFire(t *testing.T) {
	statter, read := udpSink(t, "adr")
	s := New(statter)
	s.Counter("calls").Inc(3)
	s.Timer("latency").Record(42 * time.Millisecond)

	lines := read(2)
	if len(lines) < 2 {
		t.Fatalf("captured %d lines, want >= 2: %v", len(lines), lines)
	}
	joined := strings.Join(lines, "|")
	if !strings.Contains(joined, "adr.calls:3|c") {
		t.Errorf("missing counter datagram; got %q", joined)
	}
	if !strings.Contains(joined, "adr.latency:42|ms") {
		t.Errorf("missing timing datagram; got %q", joined)
	}
}

// TestAdapterSampleRate1 pins the rate=1.0 wire form.
func TestAdapterSampleRate1(t *testing.T) {
	statter, read := udpSink(t, "")
	NewWithRate(statter, 1.0).Counter("k").Inc(1)
	lines := read(1)
	if len(lines) != 1 || lines[0] != "k:1|c" {
		t.Errorf("wire = %v, want [k:1|c]", lines)
	}
}

// TestAdapterSampleRateZero — rate=0 means "never sample". The adapter must
// not silently invert the meaning and always emit.
func TestAdapterSampleRateZero(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ListenPacket: %v", err)
	}
	defer pc.Close()

	statter, err := statsd.NewClient(pc.LocalAddr().String(), "")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer statter.Close()

	scope := NewWithRate(statter, 0)
	// 200 shots at rate 0 — statsd MUST drop them all.
	for i := 0; i < 200; i++ {
		scope.Counter("k").Inc(1)
		scope.Timer("t").Record(time.Microsecond)
	}

	if lines := readMaybe(t, pc, 200*time.Millisecond); len(lines) != 0 {
		t.Errorf("rate=0 emitted %d datagrams, want 0: %v", len(lines), lines)
	}
}

// TestAdapterNilStatter pins the nil-input contract: New(nil) degrades to
// the driver's NoopScope instead of panicking on first use.
func TestAdapterNilStatter(t *testing.T) {
	s := New(nil)
	if s != drv.NoopScope {
		t.Fatalf("New(nil) = %T, want drv.NoopScope", s)
	}
	s.Counter("k").Inc(1) // must not panic
	s.Timer("t").Record(time.Millisecond)
}
