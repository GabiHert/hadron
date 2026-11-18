package sounds

import "math"

// The tutorial's richer sounds: one small, synthesized sound per illustration, a sting per
// chapter (all four share a motif, a rising fifth then a step, in a different key), and a
// flourish for the ending. All are short, soft, and quieter than the needs-you chime.
const (
	TourSwish    = "tour_swish"     // a card sliding between columns
	TourThud     = "tour_thud"      // and landing
	TourSend     = "tour_send"      // a chat message going up
	TourBlip     = "tour_blip"      // a reply arriving
	TourAsk      = "tour_ask"       // a question popping up: a two-note rise
	TourConfirm  = "tour_confirm"   // an answer chosen: a click and a confirm
	TourSparkle  = "tour_sparkle"   // a widget building in: quick sparkle ticks
	TourTickTock = "tour_ticktock"  // a routine's clock
	TourCascade  = "tour_cascade"   // an integration pulling items in: a cascade of soft pops
	TourChirp    = "tour_chirp"     // an MCP tool call: a short digital chirp
	TourFlourish = "tour_flourish"  // the ending: the planner's work appearing
	TourBasics   = "tour_ch_basics" // chapter stings
	TourPlanner  = "tour_ch_planner"
	TourAutomate = "tour_ch_automate"
	TourMCP      = "tour_ch_mcp"
)

// richTour renders the sounds above.
var richTour = map[string]func() []float64{
	TourSwish: swish,
	TourThud:  thud,
	TourSend:  send,
	TourBlip:  blip,
	TourAsk: func() []float64 {
		return bells([]note{{freq: 783.99, start: 0, dur: 0.2, gain: 0.22}, {freq: 1046.50, start: 0.1, dur: 0.26, gain: 0.22}})
	},
	TourConfirm:  confirm,
	TourSparkle:  sparkle,
	TourTickTock: ticktock,
	TourCascade:  cascade,
	TourChirp:    chirp,
	TourFlourish: flourish,
	TourBasics:   func() []float64 { return sting(523.25, false) }, // C
	TourPlanner:  func() []float64 { return sting(587.33, false) }, // D
	TourAutomate: func() []float64 { return sting(659.25, false) }, // E
	TourMCP:      func() []float64 { return sting(783.99, true) },  // G, with a digital edge
}

// richOrder is their order in TourKinds.
var richOrder = []string{TourSwish, TourThud, TourSend, TourBlip, TourAsk, TourConfirm, TourSparkle, TourTickTock, TourCascade, TourChirp, TourFlourish, TourBasics, TourPlanner, TourAutomate, TourMCP}

// buffer is a sound being built.
type buffer []float64

func newBuffer(dur float64) buffer { return make(buffer, int(dur*float64(rate))) }

// sine adds a sine at freq (Hz, which may glide to freq2) from start for dur seconds, with a quick
// attack and an exponential decay of the given speed.
func (b buffer) sine(freq, freq2, start, dur, gain, decay float64) {
	first := int(start * rate)
	n := int(dur * rate)
	phase := 0.0
	for i := 0; i < n && first+i < len(b); i++ {
		t := float64(i) / rate
		f := freq + (freq2-freq)*(t/dur)
		phase += 2 * math.Pi * f / rate
		env := math.Min(1, t/0.004) * math.Exp(-t*decay)
		b[first+i] += gain * env * math.Sin(phase)
	}
}

// noise adds band-limited noise whose low-pass cutoff moves from cut1 to cut2, shaped by a swell.
func (b buffer) noise(start, dur, cut1, cut2, gain float64, seed uint32) {
	first := int(start * rate)
	n := int(dur * rate)
	lp, lp2 := 0.0, 0.0
	for i := 0; i < n && first+i < len(b); i++ {
		x := float64(i) / float64(n)
		seed ^= seed << 13
		seed ^= seed >> 17
		seed ^= seed << 5
		v := float64(seed)/float64(math.MaxUint32)*2 - 1
		cut := cut1 + (cut2-cut1)*x
		a := 1 - math.Exp(-2*math.Pi*cut/rate)
		lp += a * (v - lp)
		lp2 += a * (lp - lp2)
		b[first+i] += gain * math.Pow(math.Sin(math.Pi*x), 1.5) * lp2
	}
}

func swish() []float64 {
	b := newBuffer(0.22)
	b.noise(0, 0.22, 2600, 700, 0.5, 91283)
	return b
}

func thud() []float64 {
	b := newBuffer(0.16)
	b.sine(150, 70, 0, 0.16, 0.3, 26)
	b.noise(0, 0.03, 900, 400, 0.18, 7711)
	return b
}

func send() []float64 {
	b := newBuffer(0.24)
	b.noise(0, 0.24, 500, 3200, 0.38, 4242)
	b.sine(420, 900, 0.02, 0.2, 0.08, 10)
	return b
}

func blip() []float64 {
	b := newBuffer(0.12)
	b.sine(880, 990, 0, 0.12, 0.26, 30)
	return b
}

func confirm() []float64 {
	b := newBuffer(0.36)
	b.sine(2000, 2000, 0, 0.03, 0.22, 140) // the click
	b.sine(1318.51, 1318.51, 0.06, 0.28, 0.2, 12)
	b.sine(1567.98, 1567.98, 0.12, 0.24, 0.18, 12)
	return b
}

func sparkle() []float64 {
	b := newBuffer(0.4)
	pitches := []float64{2637, 3136, 2794, 3520, 3136}
	for k, f := range pitches {
		b.sine(f, f, float64(k)*0.065, 0.09, 0.13, 55)
	}
	return b
}

func ticktock() []float64 {
	b := newBuffer(0.72)
	for k := 0; k < 3; k++ {
		f := 1650.0
		if k%2 == 1 {
			f = 1150
		}
		b.sine(f, f, float64(k)*0.3, 0.05, 0.2, 110)
		b.noise(float64(k)*0.3, 0.012, 3000, 2000, 0.06, uint32(300+k))
	}
	return b
}

func cascade() []float64 {
	b := newBuffer(0.48)
	for k := 0; k < 5; k++ {
		f := 760 - float64(k)*55
		b.sine(f, f*0.55, float64(k)*0.07, 0.11, 0.24-float64(k)*0.02, 30)
	}
	return b
}

func chirp() []float64 {
	b := newBuffer(0.16)
	// Two quick stepped tones with odd harmonics: a small, friendly "data" sound.
	for k, f := range []float64{1320, 1980} {
		start := float64(k) * 0.055
		b.sine(f, f*1.06, start, 0.07, 0.16, 35)
		b.sine(3*f, 3*f, start, 0.05, 0.035, 60)
	}
	return b
}

func flourish() []float64 {
	b := newBuffer(0.9)
	// A rolled C major arpeggio with a soft shimmer on top: the planner's work landing.
	for k, f := range []float64{523.25, 659.25, 783.99, 1046.50, 1318.51} {
		start := float64(k) * 0.07
		b.sine(f, f, start, 0.6, 0.1, 7)
		b.sine(2*f, 2*f, start, 0.35, 0.02, 10)
	}
	b.noise(0.25, 0.45, 6000, 3500, 0.035, 1357)
	return b
}

// sting is a chapter's short motif: the root, a fifth above, then a step up, as soft bells. The
// MCP chapter adds a faint digital edge.
func sting(root float64, digital bool) []float64 {
	fifth := root * 1.5
	step := fifth * 9 / 8
	b := buffer(bells([]note{{freq: root, start: 0, dur: 0.26, gain: 0.2}, {freq: fifth, start: 0.09, dur: 0.28, gain: 0.19}, {freq: step, start: 0.18, dur: 0.32, gain: 0.17}}))
	if digital {
		b.sine(3*fifth, 3*fifth, 0.09, 0.08, 0.03, 40)
		b.sine(3*step, 3*step, 0.18, 0.08, 0.03, 40)
	}
	return b
}
