package pty

// ring is a fixed-size byte ring: the scrollback a terminal replays when it mounts. total counts
// every byte ever written, so a reader can tell which output it has already seen.
type ring struct {
	buf   []byte
	start int // index of the oldest byte
	size  int // bytes held
	total int64
}

func newRing(capacity int) *ring { return &ring{buf: make([]byte, capacity)} }

func (r *ring) write(p []byte) {
	r.total += int64(len(p))
	if len(p) >= len(r.buf) {
		copy(r.buf, p[len(p)-len(r.buf):])
		r.start, r.size = 0, len(r.buf)
		return
	}
	end := (r.start + r.size) % len(r.buf)
	n := copy(r.buf[end:], p)
	if n < len(p) {
		copy(r.buf, p[n:])
	}
	r.size += len(p)
	if r.size > len(r.buf) {
		r.start = (r.start + r.size - len(r.buf)) % len(r.buf)
		r.size = len(r.buf)
	}
}

// bytes returns a copy of what the ring holds, oldest first.
func (r *ring) bytes() []byte {
	out := make([]byte, r.size)
	n := copy(out, r.buf[r.start:min(r.start+r.size, len(r.buf))])
	copy(out[n:], r.buf[:r.size-n])
	return out
}

// wrapped reports whether older output has been dropped.
func (r *ring) wrapped() bool { return r.total > int64(r.size) }
