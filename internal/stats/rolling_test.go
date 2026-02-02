package stats

import "testing"

func TestRollingStats(t *testing.T) {
	r := NewRollingStats(3)
	r.Add(1)
	r.Add(2)
	r.Add(3)

	if r.Count() != 3 {
		t.Fatalf("expected count 3, got %d", r.Count())
	}
	if r.Mean() != 2 {
		t.Fatalf("expected mean 2, got %f", r.Mean())
	}

	r.Add(4)
	if r.Count() != 3 {
		t.Fatalf("expected count 3 after rollover, got %d", r.Count())
	}
	if r.Mean() != 3 {
		t.Fatalf("expected mean 3 after rollover, got %f", r.Mean())
	}
}