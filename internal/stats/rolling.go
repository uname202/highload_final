package stats

import "math"

// RollingStats keeps a fixed-size window of values and provides mean/std dev.
type RollingStats struct {
	capacity int
	values   []float64
	idx      int
	count    int
	sum      float64
	sumSq    float64
}

func NewRollingStats(capacity int) *RollingStats {
	if capacity <= 0 {
		capacity = 1
	}
	return &RollingStats{
		capacity: capacity,
		values:   make([]float64, capacity),
	}
}

func (r *RollingStats) Add(v float64) {
	if r.count < r.capacity {
		r.values[r.idx] = v
		r.sum += v
		r.sumSq += v * v
		r.idx = (r.idx + 1) % r.capacity
		r.count++
		return
	}

	old := r.values[r.idx]
	r.sum -= old
	r.sumSq -= old * old

	r.values[r.idx] = v
	r.sum += v
	r.sumSq += v * v
	r.idx = (r.idx + 1) % r.capacity
}

func (r *RollingStats) Count() int {
	return r.count
}

func (r *RollingStats) Mean() float64 {
	if r.count == 0 {
		return 0
	}
	return r.sum / float64(r.count)
}

func (r *RollingStats) StdDev() float64 {
	if r.count == 0 {
		return 0
	}
	mean := r.Mean()
	variance := r.sumSq/float64(r.count) - mean*mean
	if variance < 0 {
		variance = 0
	}
	return math.Sqrt(variance)
}

func (r *RollingStats) ZScore(v float64) float64 {
	std := r.StdDev()
	if std == 0 {
		return 0
	}
	return (v - r.Mean()) / std
}