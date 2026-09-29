package model

import "testing"

func TestQuantityAndRenewsIn(t *testing.T) {
	cases := map[string]string{
		Quantity(0, "day", "days"):                   "0 days",
		Quantity(1, "day", "days"):                   "1 day",
		Quantity(2, "day", "days"):                   "2 days",
		Quantity(1, "failed check", "failed checks"): "1 failed check",
		RenewsIn(0):  "today",
		RenewsIn(1):  "in 1 day",
		RenewsIn(12): "in 12 days",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
}
