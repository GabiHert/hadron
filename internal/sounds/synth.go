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

// The tutorial's sounds: soft and short, all well under a second.
const (
	TourGlide = "tour_glide" // a breathy whoosh when the spotlight moves
	TourTick  = "tour_tick"  // a light tick when the step changes
	TourPop   = "tour_pop"   // a small pop when a card appears in an illustration
	TourChime = "tour_chime" // a gentle rising chime at the end
)

// TourKinds lists the tutorial's sounds: the step sounds, then the richer ones in tour.go.
var TourKinds = append([]string{TourGlide, TourTick, TourPop, TourChime}, richOrder...)

// AllKinds is every sound the player can play.
func AllKinds() []string { return append(append([]string{}, Kinds...), TourKinds...) }

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

// bells renders notes as small bells: a quick attack, an exponential decay, a soft octave overtone.
func bells(notes []note) []float64 {
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
			env := math.Min(1, t/0.006) * math.Exp(-t*7/n.dur)
			v := math.Sin(2*math.Pi*n.freq*t) + 0.25*math.Sin(2*math.Pi*2*n.freq*t)
			samples[first+i] += n.gain * env * v / 1.25
		}
	}
	return samples
}

// glide is a soft whoosh: noise through a low-pass whose cutoff sweeps up and back, under a faint
// rising tone, swelling in and out over 0.3 s. The noise is seeded, so the file is always the same.
func glide() []float64 {
	dur := 0.3
	out := make([]float64, int(dur*float64(rate)))
	seed := uint32(2463534242)
	lp := 0.0
	phase := 0.0
	for i := range out {
		t := float64(i) / rate
		x := t / dur
		seed ^= seed << 13
		seed ^= seed >> 17
		seed ^= seed << 5
		noise := float64(seed)/float64(math.MaxUint32)*2 - 1
		// The low-pass opens to about 2.5 kHz in the middle, a breath rather than a hiss.
		cut := 300 + 2200*math.Sin(math.Pi*x)
		a := 1 - math.Exp(-2*math.Pi*cut/rate)
		lp += a * (noise - lp)
		phase += 2 * math.Pi * (320 + 260*x) / rate
		env := math.Pow(math.Sin(math.Pi*x), 2)
		out[i] = env * (0.42*lp + 0.05*math.Sin(phase))
	}
	return out
}

// tick is a light, very short click: a high sine that dies in a few milliseconds.
func tick() []float64 {
	dur := 0.045
	out := make([]float64, int(dur*float64(rate)))
	for i := range out {
		t := float64(i) / rate
		env := math.Min(1, t/0.0015) * math.Exp(-t*90)
		out[i] = 0.32 * env * (math.Sin(2*math.Pi*1900*t) + 0.3*math.Sin(2*math.Pi*3800*t)) / 1.3
	}
	return out
}

// pop is a small bubble: a sine whose pitch drops fast, with a quick decay.
func pop() []float64 {
	dur := 0.11
	out := make([]float64, int(dur*float64(rate)))
	phase := 0.0
	for i := range out {
		t := float64(i) / rate
		f := 420 + 520*math.Exp(-t*45)
		phase += 2 * math.Pi * f / rate
		env := math.Min(1, t/0.003) * math.Exp(-t*32)
		out[i] = 0.3 * env * math.Sin(phase)
	}
	return out
}

func render(kind string) []float64 {
	if f, ok := richTour[kind]; ok {
		return f()
	}
	switch kind {
	case TourGlide:
		return glide()
	case TourTick:
		return tick()
	case TourPop:
		return pop()
	case TourChime:
		// C6, E6, G6 rolled upwards, quiet: done, without fanfare.
		return bells([]note{{freq: 1046.50, start: 0, dur: 0.42, gain: 0.26}, {freq: 1318.51, start: 0.08, dur: 0.44, gain: 0.24}, {freq: 1567.98, start: 0.16, dur: 0.5, gain: 0.22}})
	}
	return bells(score(kind))
}

// Duration is how long a sound lasts, in seconds.
func Duration(kind string) float64 { return float64(len(render(kind))) / rate }

// WAV renders a sound as a 16-bit mono WAV file.
func WAV(kind string) []byte {
	samples := render(kind)
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
