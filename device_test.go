package main

import (
	"math"
	"testing"
)

func TestBandRoundTrip(t *testing.T) {
	cases := []Band{
		{Freq: 30, Gain: -4.0, Q: 0.3, Type: "PK"},
		{Freq: 105, Gain: 5.5, Q: 0.7, Type: "LSH"},
		{Freq: 20000, Gain: 12.0, Q: 10.0, Type: "HSH"},
		{Freq: 20, Gain: -24.0, Q: 0.05, Type: "LPF"},
		{Freq: 1000, Gain: 0, Q: 0.707, Type: "HPF"},
		{Freq: 7500, Gain: -6.0, Q: 0.9, Type: "PK"},
	}
	for _, in := range cases {
		a, b, err := encodeBand(in)
		if err != nil {
			t.Fatalf("encode %+v: %v", in, err)
		}
		out := decodeBand(a, b)
		if out.Freq != in.Freq {
			t.Errorf("freq: got %d want %d", out.Freq, in.Freq)
		}
		if math.Abs(out.Gain-in.Gain) > 0.051 {
			t.Errorf("gain: got %.2f want %.2f", out.Gain, in.Gain)
		}
		if math.Abs(out.Q-in.Q) > 0.0011 {
			t.Errorf("q: got %.3f want %.3f", out.Q, in.Q)
		}
		if out.Type != in.Type {
			t.Errorf("type: got %q want %q", out.Type, in.Type)
		}
	}
}

func TestBandClamp(t *testing.T) {
	a, b, err := encodeBand(Band{Freq: 5, Gain: 99, Q: 50, Type: "PK"})
	if err != nil {
		t.Fatal(err)
	}
	out := decodeBand(a, b)
	if out.Freq != 20 {
		t.Errorf("freq should clamp to 20, got %d", out.Freq)
	}
	if out.Gain != 24 {
		t.Errorf("gain should clamp to +24 dB, got %.1f", out.Gain)
	}
	if out.Q != 20 {
		t.Errorf("q should clamp to 20, got %.2f", out.Q)
	}
}

func TestBandBadType(t *testing.T) {
	if _, _, err := encodeBand(Band{Freq: 100, Gain: 1, Q: 1, Type: "SHELF"}); err == nil {
		t.Fatal("expected error for unknown filter type")
	}
}

func TestPresetsShape(t *testing.T) {
	all := append([]Preset{{ID: "factory", Name: "factory", Bands: factoryDAC.Bands}}, presets...)
	for _, p := range all {
		if len(p.Bands) != numBands {
			t.Errorf("%s: %d bands, want %d", p.ID, len(p.Bands), numBands)
		}
		for i, b := range p.Bands {
			if b.Freq < 20 || b.Freq > 20000 {
				t.Errorf("%s band %d: freq %d out of range", p.ID, i, b.Freq)
			}
			if b.Q <= 0 {
				t.Errorf("%s band %d: q must be > 0", p.ID, i)
			}
			if _, ok := typeID[b.Type]; !ok {
				t.Errorf("%s band %d: bad type %q", p.ID, i, b.Type)
			}
		}
	}
}

func TestBankMap(t *testing.T) {
	for _, bank := range []string{"dac", "adc"} {
		cfg, ok := bankMap[bank]
		if !ok {
			t.Fatalf("missing %s bank", bank)
		}
		// 5 bands × 2 registers must fit below the next reserved block.
		end := cfg.Base + uint8(numBands*2)
		if end > 0x30 {
			t.Errorf("%s bank overruns register space: ends at 0x%02X", bank, end)
		}
	}
}
