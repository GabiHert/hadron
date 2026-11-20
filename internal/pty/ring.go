package pty

// ring is a byte ring of at most max bytes: the scrollback a terminal replays when it mounts. It
// starts small and grows as output arrives, so a quiet session does not hold the whole capacity.
// total counts every byte ever written, so a reader can tell which output it has already seen.
type ring struct {
	buf   []byte
	max   int
	start int // index of the oldest byte
	size  int // bytes held
	total int64
}

// ringMinGrow is the smallest allocation the ring grows to.
const ringMinGrow = 64 << 10

func newRing(capacity int) *ring { return &ring{max: capacity} }

func (r *ring) write(p []byte) {
	r.total += int64(len(p))
	if len(r.buf) < r.max {
		// Until it is full size the ring has never wrapped, so its bytes start at index 0.
		need := r.size + len(p)
		if need <= r.max {
			if need > len(r.buf) {
				r.grow(min(r.max, max(need, 2*len(r.buf), ringMinGrow)))
			}
			copy(r.buf[r.size:], p)
			r.size = need
			return
		}
		r.grow(r.max)
	}
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

func (r *ring) grow(n int) {
	buf := make([]byte, n)
	copy(buf, r.buf[:r.size])
	r.buf = buf
}

// bytes returns a copy of what the ring holds, oldest first.
func (r *ring) bytes() []byte {
	out := make([]byte, r.size)
	if r.size == 0 {
		return out
	}
	n := copy(out, r.buf[r.start:min(r.start+r.size, len(r.buf))])
	copy(out[n:], r.buf[:r.size-n])
	return out
}

// tail returns a copy of the newest n bytes the ring holds (all of them when it holds fewer).
func (r *ring) tail(n int) []byte {
	if n >= r.size {
		return r.bytes()
	}
	out := make([]byte, n)
	from := (r.start + r.size - n) % len(r.buf)
	k := copy(out, r.buf[from:min(from+n, len(r.buf))])
	copy(out[k:], r.buf[:n-k])
	return out
}

// wrapped reports whether older output has been dropped.
func (r *ring) wrapped() bool { return r.total > int64(r.size) }
