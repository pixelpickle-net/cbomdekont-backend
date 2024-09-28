package http

import (
	"github.com/gofiber/fiber/v3"
	"github.com/prometheus/client_golang/prometheus"
	"strconv"
	"time"
)

type PrometheusMiddleware struct {
	Histogram *prometheus.HistogramVec
	Counter   *prometheus.CounterVec
}

func NewPrometheusMiddleware() *PrometheusMiddleware {
	histogram := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Subsystem: "http",
		Name:      "request_duration_seconds",
		Help:      "The HTTP request latencies in seconds.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"method", "path", "status"})

	counter := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: "http",
			Name:      "requests_total",
			Help:      "The total number of HTTP requests.",
		}, []string{"status"})

	//must register
	prometheus.MustRegister(histogram)
	prometheus.MustRegister(counter)

	return &PrometheusMiddleware{
		Histogram: histogram,
		Counter:   counter,
	}
}

// Metrics godoc
// @Summary Prometheus metrics
// @Description returns HTTP requests duration and Go runtime metrics
// @Tags Kubernetes
// @Produce plain
// @Router /metrics [get]
// @Success 200 {string} string "OK"
func (p *PrometheusMiddleware) Handler(c fiber.Ctx) error {
	begin := time.Now()
	err := c.Next()

	duration := time.Since(begin)
	status := strconv.Itoa(c.Response().StatusCode())
	method := c.Method()
	path := c.Path()

	p.Histogram.WithLabelValues(method, path, status).Observe(duration.Seconds())
	p.Counter.WithLabelValues(status).Inc()

	return err
}
