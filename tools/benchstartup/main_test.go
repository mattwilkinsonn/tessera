package main

import (
	"bytes"
	"testing"
	"time"
)

func TestRunRequiresTwoBinariesAndPositiveSampleCount(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
	}{
		{name: "missing binary", args: []string{}},
		{name: "one binary", args: []string{"base"}},
		{name: "zero count", args: []string{"-n", "0", "base", "new"}},
		{name: "non-integer count", args: []string{"-n", "many", "base", "new"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stderr bytes.Buffer
			if err := run(t.Context(), test.args, &bytes.Buffer{}, &stderr); err == nil {
				t.Fatal("run() succeeded; want argument error")
			}
		})
	}
}

func TestEvaluateBarsAppliesEachThreshold(t *testing.T) {
	ms := time.Millisecond
	rows := func(newSnapP50, snapP90, focusP90, helpP50 time.Duration) []measurement {
		return []measurement{
			{binary: "base", command: "snap 3col", p50: 20 * ms},
			{binary: "base", command: "focus east", p50: 20 * ms},
			{binary: "new", command: "snap 3col", p50: newSnapP50, p90: snapP90},
			{binary: "new", command: "focus east", p50: 10 * ms, p90: focusP90},
			{binary: "new", command: "snap --help", p50: helpP50},
		}
	}
	for _, test := range []struct {
		name   string
		rows   []measurement
		size   int64
		failed string
	}{
		{name: "all bars pass at their limits", rows: rows(40*ms, 45*ms, 45*ms, 60*ms), size: 70_000_000},
		{name: "p50 ratio above 2x fails", rows: rows(41*ms, 45*ms, 45*ms, 60*ms), size: 1, failed: "hot-path p50 ratio <= 2.0x"},
		{name: "snap p90 above 45 ms fails", rows: rows(10*ms, 46*ms, 10*ms, 60*ms), size: 1, failed: "hot-path p90 <= 45 ms"},
		{name: "focus p90 above 45 ms fails", rows: rows(10*ms, 10*ms, 46*ms, 60*ms), size: 1, failed: "hot-path p90 <= 45 ms"},
		{name: "help above 60 ms fails", rows: rows(10*ms, 10*ms, 10*ms, 61*ms), size: 1, failed: "help p50 <= 60 ms"},
		{name: "binary above 70 MB fails", rows: rows(10*ms, 10*ms, 10*ms, 10*ms), size: 70_000_001, failed: "binary size <= 70 MB"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, bars := evaluateBars(test.rows, test.size)
			for _, bar := range bars {
				if bar.pass != (bar.name != test.failed) {
					t.Fatalf("bar %q pass = %t", bar.name, bar.pass)
				}
			}
		})
	}
}
