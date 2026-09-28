//go:build windows

package driver

import (
	"math"
	"testing"
)

func TestNormalizeTSMasterBusLoad(t *testing.T) {
	tests := []struct {
		name    string
		percent float64
		want    float64
	}{
		{name: "zero", percent: 0, want: 0},
		{name: "percentage", percent: 37.5, want: 0.375},
		{name: "full", percent: 100, want: 1},
		{name: "clamped", percent: 120, want: 1},
		{name: "negative", percent: -1, want: 0},
		{name: "nan", percent: math.NaN(), want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeTSMasterBusLoad(tt.percent); got != tt.want {
				t.Fatalf("normalizeTSMasterBusLoad(%v) = %v, want %v", tt.percent, got, tt.want)
			}
		})
	}
}
