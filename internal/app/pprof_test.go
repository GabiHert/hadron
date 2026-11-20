package app

import "testing"

func TestPprofOnlyOnLoopbackAndOnlyWhenAsked(t *testing.T) {
	for v, want := range map[string]bool{
		"":                 false,
		"127.0.0.1:6061":   true,
		"localhost:6061":   true,
		"[::1]:6061":       true,
		"0.0.0.0:6061":     false,
		"192.168.1.5:6061": false,
		":6061":            false,
		"6061":             false,
	} {
		if _, ok := pprofAddr(v); ok != want {
			t.Errorf("pprofAddr(%q) = %v, want %v", v, ok, want)
		}
	}
}
