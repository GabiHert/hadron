// Package sounds plays Hadron's two UI sounds: a rising chime when a card moves into a needs-you
// column, and a soft low note when it moves out. The sounds are synthesized (no files to bundle,
// no network) and played with macOS's afplay, so they play while the window is hidden.
package sounds

import (
	"bytes"
	"encoding/binary"
	"math"
)

// The two sounds.
const (
	NeedsYou   = "needs_you"
	BackToWork = "back_to_work"
)

// Kinds lists them, for validation.
var Kinds = []string{NeedsYou, BackToWork}

const rate = 44100

// note is one tone: a frequency, when it starts and how long it rings, in seconds.
type note struct {
	freq, start, dur, gain float64
}

// Score is the notes of a sound.
func score(kind string) []note {
	if kind == BackToWork {
		// One soft G4, a little longer, quieter.
		return []note{{freq: 392.00, start: 0, dur: 0.32, gain: 0.45}}
	}
	// E5 then A5: a rising fourth, bright but not urgent.
	return []note{{freq: 659.25, start: 0, dur: 0.22, gain: 0.5}, {freq: 880.00, start: 0.12, dur: 0.34, gain: 0.5}}
}

// WAV renders a sound as a 16-bit mono WAV file.
func WAV(kind string) []byte {
	notes := score(kind)
	end := 0.0
	for _, n := range notes {
		end = math.Max(end, n.start+n.dur)
	}
	samples := make([]float64, int((end+0.02)*rate))
	for _, n := range notes {
		first := int(n.start * rate)
		count := int(n.dur * rate)
		for i := 0; i < count && first+i < len(samples); i++ {
			t := float64(i) / rate
			// A quick attack and an exponential decay, with a soft octave overtone: a small bell.
			env := math.Min(1, t/0.006) * math.Exp(-t*7/n.dur)
			v := math.Sin(2*math.Pi*n.freq*t) + 0.25*math.Sin(2*math.Pi*2*n.freq*t)
			samples[first+i] += n.gain * env * v / 1.25
		}
	}
	var pcm bytes.Buffer
	for _, s := range samples {
		_ = binary.Write(&pcm, binary.LittleEndian, int16(math.Max(-1, math.Min(1, s))*32000))
	}
	var b bytes.Buffer
	w := func(v any) { _ = binary.Write(&b, binary.LittleEndian, v) }
	b.WriteString("RIFF")
	w(uint32(36 + pcm.Len()))
	b.WriteString("WAVEfmt ")
	w(uint32(16))
	w(uint16(1)) // PCM
	w(uint16(1)) // mono
	w(uint32(rate))
	w(uint32(rate * 2))
	w(uint16(2))
	w(uint16(16))
	b.WriteString("data")
	w(uint32(pcm.Len()))
	b.Write(pcm.Bytes())
	return b.Bytes()
}
