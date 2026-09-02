package interview

import "testing"

// TestClassifyAnswersCarryingMoneyWords: the 2026-08-03 defect. An answer that
// DESCRIBES money handling must not be routed as a cost question — that
// swallowed the real answer, re-put the item, and desynchronised the whole
// sitting (the scrambled c1 submissions).
func TestClassifyAnswersCarryingMoneyWords(t *testing.T) {
	answers := []string{
		"Lines go in, each with a quantity and a unit price in pennies.",
		"An invoice line holds a quantity and a unit price in integer pennies.",
		"When a line is malformed, the cost of guessing is wrong totals, so it is refused instead.",
	}
	for _, a := range answers {
		if got := Classify(a); got != NoTopic {
			t.Errorf("Classify(%q) = %v, want NoTopic — describing money is not asking about money", a, got)
		}
	}
	questions := []string{
		"how much will this cost",
		"price?",
		"what's the price",
		"What will it cost me",
	}
	for _, q := range questions {
		if got := Classify(q); got != TopicCost {
			t.Errorf("Classify(%q) = %v, want TopicCost — a real cost question must still be met", q, got)
		}
	}
}
