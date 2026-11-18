package sounds

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"regexp"
	"slices"
	"path/filepath"
	"testing"
)

// The tutorial's sounds stay short, quiet, small and the same every time they are made.
func TestTourSoundsAreShortSoftAndStable(t *testing.T) {
	chime, chimeEnergy := peakOf(WAV(NeedsYou)), energy(WAV(NeedsYou))
	if len(TourKinds) != 19 {
		t.Errorf("%d tour sounds", len(TourKinds))
	}
	for _, k := range TourKinds {
		d := Duration(k)
		if d <= 0 || d >= 1 {
			t.Errorf("%s lasts %.2fs, want under a second", k, d)
		}
		w := WAV(k)
		if string(w[:4]) != "RIFF" || string(w[8:12]) != "WAVE" {
			t.Fatalf("%s is not a WAV file", k)
		}
		if len(w) > 90_000 {
			t.Errorf("%s is %d bytes, want it small", k, len(w))
		}
		if !bytes.Equal(w, WAV(k)) {
			t.Errorf("%s changes between renders", k)
		}
		peak := peakOf(w)
		// Never louder than the needs-you chime: they play many times in one tour.
		if peak < 0.05 || peak > chime {
			t.Errorf("%s peaks at %.2f, want a soft sound between 0.05 and the needs-you chime's %.2f", k, peak, chime)
		}
		if e := energy(w); e > chimeEnergy {
			t.Errorf("%s carries %.4f of energy, more than the needs-you chime's %.4f", k, e, chimeEnergy)
		}
	}
	p := &Player{Dir: t.TempDir()}
	for _, k := range AllKinds() {
		path, err := p.File(k)
		if err != nil || filepath.Base(path) != k+".wav" {
			t.Errorf("%s: %s %v", k, path, err)
		}
	}
	if _, err := p.File("trumpet"); err == nil {
		t.Error("an unknown sound has no file")
	}
}

func samplesOf(w []byte) []float64 {
	pcm := w[44:]
	out := make([]float64, 0, len(pcm)/2)
	for i := 0; i+1 < len(pcm); i += 2 {
		out = append(out, float64(int16(binary.LittleEndian.Uint16(pcm[i:])))/32000)
	}
	return out
}

func peakOf(w []byte) float64 {
	p := 0.0
	for _, v := range samplesOf(w) {
		p = math.Max(p, math.Abs(v))
	}
	return p
}

// energy is the sum of squares over the sound, in seconds: how loud it feels over its length.
func energy(w []byte) float64 {
	e := 0.0
	for _, v := range samplesOf(w) {
		e += v * v
	}
	return e / rate
}

// Every sound the tutorial asks for exists here, so a typo cannot go silent.
func TestTheTutorialNamesOnlySoundsThatExist(t *testing.T) {
	raw, err := os.ReadFile("../../frontend/src/components/Tutorial.tsx")
	if err != nil {
		t.Fatal(err)
	}
	names := regexp.MustCompile(`"(tour_[a-z_]+)"`).FindAllStringSubmatch(string(raw), -1)
	if len(names) < 15 {
		t.Fatalf("found only %d sound names in Tutorial.tsx", len(names))
	}
	for _, m := range names {
		if !slices.Contains(TourKinds, m[1]) {
			t.Errorf("Tutorial.tsx plays %q, which internal/sounds does not have", m[1])
		}
	}
}
