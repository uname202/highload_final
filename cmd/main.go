package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"math"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"highload_final/internal/analytics"
	"highload_final/internal/model"
	"highload_final/internal/storage"
)

const (
	defaultHTTPAddr   = ":8080"
	defaultRedisAddr  = "127.0.0.1:6379"
	defaultRedisDB    = 0
	defaultWindowSize = 50
	defaultThreshold  = 2.0
)

type server struct {
	store      *storage.RedisStore
	engine     *analytics.Engine
	metricsCh  chan model.Metric
	ctx        context.Context
	cancel     context.CancelFunc
	windowSize int
	threshold  float64

	metricIngested   prometheus.Counter
	metricBadRequest prometheus.Counter
	metricQueueFull  prometheus.Counter
	metricRedisErr   prometheus.Counter
	metricProcTime   prometheus.Histogram
	metricAvgCPU     prometheus.Gauge
	metricAvgRPS     prometheus.Gauge
	metricZCPU       prometheus.Gauge
	metricZRPS       prometheus.Gauge
	metricAnomCPU    prometheus.Gauge
	metricAnomRPS    prometheus.Gauge
	metricWinCount   prometheus.Gauge
	metricAnomCPUCnt prometheus.Counter
	metricAnomRPSCnt prometheus.Counter
}

func main() {
	cfg := loadConfig()

	store := storage.NewRedisStore(cfg.redisAddr, cfg.redisPassword, cfg.redisDB)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := store.Ping(ctx); err != nil {
		log.Printf("redis ping failed: %v", err)
	}

	engine := analytics.NewEngine(cfg.windowSize, cfg.threshold)

	srv := newServer(ctx, store, engine, cfg.windowSize, cfg.threshold)
	go srv.runWorker()

	httpServer := &http.Server{
		Addr:              cfg.httpAddr,
		Handler:           srv.routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("http server listening on %s", cfg.httpAddr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("listen failed: %v", err)
		}
	}()

	waitForShutdown(cancel)

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("http shutdown error: %v", err)
	}
	if err := store.Close(); err != nil {
		log.Printf("redis close error: %v", err)
	}
}

type config struct {
	httpAddr      string
	redisAddr     string
	redisPassword string
	redisDB       int
	windowSize    int
	threshold     float64
}

func loadConfig() config {
	return config{
		httpAddr:      getEnv("HTTP_ADDR", defaultHTTPAddr),
		redisAddr:     getEnv("REDIS_ADDR", defaultRedisAddr),
		redisPassword: getEnv("REDIS_PASSWORD", ""),
		redisDB:       getEnvInt("REDIS_DB", defaultRedisDB),
		windowSize:    getEnvInt("ANALYTICS_WINDOW", defaultWindowSize),
		threshold:     getEnvFloat("ANALYTICS_THRESHOLD", defaultThreshold),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			return parsed
		}
	}
	return fallback
}

func getEnvFloat(key string, fallback float64) float64 {
	if v := os.Getenv(key); v != "" {
		if parsed, err := strconv.ParseFloat(v, 64); err == nil {
			return parsed
		}
	}
	return fallback
}

func newServer(ctx context.Context, store *storage.RedisStore, engine *analytics.Engine, windowSize int, threshold float64) *server {
	metricsCh := make(chan model.Metric, 1024)

	s := &server{
		store:      store,
		engine:     engine,
		metricsCh:  metricsCh,
		windowSize: windowSize,
		threshold:  threshold,
	}
	s.ctx, s.cancel = context.WithCancel(ctx)

	s.metricIngested = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "ingest_total",
		Help: "Total metrics ingested",
	})
	s.metricBadRequest = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "ingest_bad_request_total",
		Help: "Total bad ingest requests",
	})
	s.metricQueueFull = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "ingest_queue_full_total",
		Help: "Total ingest requests rejected because queue is full",
	})
	s.metricRedisErr = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "redis_error_total",
		Help: "Total redis errors",
	})
	s.metricProcTime = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "ingest_processing_seconds",
		Help:    "Latency for processing metrics",
		Buckets: prometheus.DefBuckets,
	})
	s.metricAvgCPU = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "analytics_rolling_avg_cpu",
		Help: "Rolling average of CPU",
	})
	s.metricAvgRPS = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "analytics_rolling_avg_rps",
		Help: "Rolling average of RPS",
	})
	s.metricZCPU = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "analytics_zscore_cpu",
		Help: "Z-score for CPU",
	})
	s.metricZRPS = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "analytics_zscore_rps",
		Help: "Z-score for RPS",
	})
	s.metricAnomCPU = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "analytics_anomaly_cpu",
		Help: "CPU anomaly flag (1 if anomaly)",
	})
	s.metricAnomRPS = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "analytics_anomaly_rps",
		Help: "RPS anomaly flag (1 if anomaly)",
	})
	s.metricWinCount = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "analytics_window_count",
		Help: "Number of samples in analytics window",
	})
	s.metricAnomCPUCnt = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "analytics_anomaly_cpu_total",
		Help: "Total CPU anomalies detected",
	})
	s.metricAnomRPSCnt = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "analytics_anomaly_rps_total",
		Help: "Total RPS anomalies detected",
	})

	prometheus.MustRegister(
		s.metricIngested,
		s.metricBadRequest,
		s.metricQueueFull,
		s.metricRedisErr,
		s.metricProcTime,
		s.metricAvgCPU,
		s.metricAvgRPS,
		s.metricZCPU,
		s.metricZRPS,
		s.metricAnomCPU,
		s.metricAnomRPS,
		s.metricWinCount,
		s.metricAnomCPUCnt,
		s.metricAnomRPSCnt,
	)

	return s
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/ingest", s.handleIngest)
	mux.HandleFunc("/analytics", s.handleAnalytics)
	mux.HandleFunc("/latest", s.handleLatest)
	return mux
}

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if err := s.store.Ping(ctx); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("redis unavailable"))
		return
	}

	_, _ = w.Write([]byte("ok"))
}

func (s *server) handleIngest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	start := time.Now()
	defer func() {
		s.metricProcTime.Observe(time.Since(start).Seconds())
	}()

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var metric model.Metric
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&metric); err != nil {
		s.metricBadRequest.Inc()
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("invalid json"))
		return
	}

	if !validMetric(metric) {
		s.metricBadRequest.Inc()
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("invalid metric values"))
		return
	}

	if metric.Timestamp == 0 {
		metric.Timestamp = time.Now().Unix()
	}

	select {
	case s.metricsCh <- metric:
		s.metricIngested.Inc()
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("accepted"))
	default:
		s.metricQueueFull.Inc()
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("queue full"))
	}
}

func validMetric(m model.Metric) bool {
	if math.IsNaN(m.CPU) || math.IsNaN(m.RPS) {
		return false
	}
	if math.IsInf(m.CPU, 0) || math.IsInf(m.RPS, 0) {
		return false
	}
	return true
}

func (s *server) handleAnalytics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	res := s.engine.Latest()
	writeJSON(w, res)
}

func (s *server) handleLatest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	deviceID := r.URL.Query().Get("device_id")
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	metric, err := s.store.LoadLatest(ctx, deviceID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("redis error"))
		return
	}
	if metric == nil {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("no data"))
		return
	}

	writeJSON(w, metric)
}

func (s *server) runWorker() {
	for {
		select {
		case <-s.ctx.Done():
			return
		case metric := <-s.metricsCh:
			ctx, cancel := context.WithTimeout(s.ctx, 2*time.Second)
			if err := s.store.StoreMetric(ctx, metric); err != nil {
				s.metricRedisErr.Inc()
				log.Printf("redis store error: %v", err)
			}
			cancel()

			result := s.engine.Update(metric)
			s.updateAnalyticsMetrics(result)
		}
	}
}

func (s *server) updateAnalyticsMetrics(res analytics.Result) {
	s.metricAvgCPU.Set(res.RollingAvgCPU)
	s.metricAvgRPS.Set(res.RollingAvgRPS)
	s.metricZCPU.Set(res.ZScoreCPU)
	s.metricZRPS.Set(res.ZScoreRPS)
	if res.AnomalyCPU {
		s.metricAnomCPU.Set(1)
		s.metricAnomCPUCnt.Inc()
	} else {
		s.metricAnomCPU.Set(0)
	}
	if res.AnomalyRPS {
		s.metricAnomRPS.Set(1)
		s.metricAnomRPSCnt.Inc()
	} else {
		s.metricAnomRPS.Set(0)
	}
	s.metricWinCount.Set(float64(res.WindowCount))
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(v); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func waitForShutdown(cancel context.CancelFunc) {
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	cancel()
	log.Printf("shutdown signal received")
}
