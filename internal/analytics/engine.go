package analytics

import (
	"sync"
	"time"

	"highload_final/internal/model"
	"highload_final/internal/stats"
)

type Result struct {
	Timestamp     int64   `json:"timestamp"`
	RollingAvgCPU float64 `json:"rolling_avg_cpu"`
	RollingAvgRPS float64 `json:"rolling_avg_rps"`
	ZScoreCPU     float64 `json:"zscore_cpu"`
	ZScoreRPS     float64 `json:"zscore_rps"`
	AnomalyCPU    bool    `json:"anomaly_cpu"`
	AnomalyRPS    bool    `json:"anomaly_rps"`
	WindowCount   int     `json:"window_count"`
}

type Engine struct {
	cpuStats  *stats.RollingStats
	rpsStats  *stats.RollingStats
	threshold float64

	mu     sync.RWMutex
	latest Result
}

func NewEngine(window int, threshold float64) *Engine {
	return &Engine{
		cpuStats:  stats.NewRollingStats(window),
		rpsStats:  stats.NewRollingStats(window),
		threshold: threshold,
	}
}

func (e *Engine) Update(m model.Metric) Result {
	if m.Timestamp == 0 {
		m.Timestamp = time.Now().Unix()
	}

	e.cpuStats.Add(m.CPU)
	e.rpsStats.Add(m.RPS)

	zCPU := e.cpuStats.ZScore(m.CPU)
	zRPS := e.rpsStats.ZScore(m.RPS)

	res := Result{
		Timestamp:     m.Timestamp,
		RollingAvgCPU: e.cpuStats.Mean(),
		RollingAvgRPS: e.rpsStats.Mean(),
		ZScoreCPU:     zCPU,
		ZScoreRPS:     zRPS,
		AnomalyCPU:    abs(zCPU) >= e.threshold,
		AnomalyRPS:    abs(zRPS) >= e.threshold,
		WindowCount:   e.cpuStats.Count(),
	}

	e.mu.Lock()
	e.latest = res
	e.mu.Unlock()

	return res
}

func (e *Engine) Latest() Result {
	e.mu.RLock()
	res := e.latest
	e.mu.RUnlock()
	return res
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}