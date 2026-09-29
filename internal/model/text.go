package model

import "strconv"

// Quantity renders a count with its noun, singular for exactly one: "1 day", "3 days".
// Shared by messages, reports and logs so none of them says "1 days".
func Quantity(n int, one, many string) string {
	noun := many
	if n == 1 {
		noun = one
	}
	return strconv.Itoa(n) + " " + noun
}

// RenewsIn phrases how far off a renewal is, for "renews ..." sentences: "today",
// "in 1 day", "in 5 days".
func RenewsIn(days int) string {
	if days <= 0 {
		return "today"
	}
	return "in " + Quantity(days, "day", "days")
}
