package main

import "testing"

func TestGroupThousands(t *testing.T) {
	for n, want := range map[int64]string{100: "100", 2500: "2 500", 25000: "25 000", 1250000: "1 250 000"} {
		if got := groupThousands(n); got != want {
			t.Errorf("%d: %q", n, got)
		}
	}
}

func TestParseAmount(t *testing.T) {
	for q, want := range map[string]int64{"25 000": 25000, "25000": 25000, "25к": 25000, "2,5к": 2500, "25 000 ₽": 25000, "7 200 руб": 7200,
		"25": 0, "мед": 0, "25 000 правки": 0, "": 0} {
		if got := parseAmount(q); got != want {
			t.Errorf("%q: %d, want %d", q, got, want)
		}
	}
}
