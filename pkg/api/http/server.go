package http

import (
	"context"
	"fmt"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/adaptor"
	"github.com/gofiber/fiber/v3/middleware/cors" // Yeni import
	"github.com/gomodule/redigo/redis"
	"github.com/mehmetsafabenli/cbomdekont/pkg/fscache"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"net/http"
	"os"
	"sync/atomic"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

var (
	healthy int32
	ready   int32
	watcher *fscache.Watcher
)

type Config struct {
	HttpClientTimeout     time.Duration `mapstructure:"http-client-timeout"`
	HttpServerTimeout     time.Duration `mapstructure:"http-server-timeout"`
	ServerShutdownTimeout time.Duration `mapstructure:"server-shutdown-timeout"`
	ConfigPath            string        `mapstructure:"config-path"`
	PortMetrics           int           `mapstructure:"port-metrics"`
	Hostname              string        `mapstructure:"hostname"`
	Host                  string        `mapstructure:"host"`
	Port                  string        `mapstructure:"port"`
	H2C                   bool          `mapstructure:"h2c"`
	Unhealthy             bool          `mapstructure:"unhealthy"`
	Unready               bool          `mapstructure:"unready"`
	CacheServer           string        `mapstructure:"cache-server"`
}

type Server struct {
	app            *fiber.App
	logger         *zap.Logger
	config         *Config
	pool           *redis.Pool
	awsService     *AWSService
	tracer         trace.Tracer
	tracerProvider *sdktrace.TracerProvider
}

func NewServer(config *Config, logger *zap.Logger, aws *AWSService) (*Server, error) {
	app := fiber.New(fiber.Config{
		IdleTimeout: 2 * config.HttpServerTimeout,
	})
	srv := &Server{
		app:        app,
		logger:     logger,
		config:     config,
		awsService: aws,
	}
	return srv, nil
}

func (s *Server) ListenAndServe() (*fiber.App, *int32, *int32) {
	ctx := context.Background()

	go s.startMetricsServer()
	s.registerMiddlewares()
	s.initTracer(ctx)
	s.registerHandlers()

	// load configs in memory and start watching for changes in the config dir
	if stat, err := os.Stat(s.config.ConfigPath); err == nil && stat.IsDir() {
		var err error
		watcher, err = fscache.NewWatch(s.config.ConfigPath)
		if err != nil {
			s.logger.Error("config watch error", zap.Error(err), zap.String("path", s.config.ConfigPath))
		} else {
			watcher.Watch()
		}
	}

	// start redis connection pool
	ticker := time.NewTicker(30 * time.Second)
	s.startCachePool(ticker)

	// create the http server
	srv := s.startServer()

	// signal Kubernetes the server is ready to receive traffic
	if !s.config.Unhealthy {
		atomic.StoreInt32(&healthy, 1)
	}
	if !s.config.Unready {
		atomic.StoreInt32(&ready, 1)
	}

	return srv, &healthy, &ready
}

func (s *Server) startServer() *fiber.App {

	// determine if the port is specified
	if s.config.Port == "0" {
		// move on immediately
		return nil
	}

	// start the server in the background
	go func() {
		if err := s.app.Listen(fmt.Sprintf("%s:%s", s.config.Host, s.config.Port)); err != nil {
			s.logger.Fatal("HTTP server crashed", zap.Error(err))
		}
	}()

	// return the server and routine
	return s.app
}

func (s *Server) registerHandlers() {

	//create api group for v1
	v1 := s.app.Group("/api/v1")
	v1.Get("/metrics", adaptor.HTTPHandler(promhttp.Handler()))
	//s.app.Get("/debug/pprof/", pprof.New())
	v1.Get("/healthz", s.healthzHandler)

	v1.Post("/test", s.testTextractorHandler)

	// Preflight isteklerini ele alın
}

func (s *Server) registerMiddlewares() {
	// CORS middleware'ini güncelleyin
	s.app.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"http://57.129.41.91:9091", "http://localhost:5173", "http://localhost:3000", "https://pixelpickle.net"},
		AllowMethods:     []string{"GET", "POST", "HEAD", "PUT", "DELETE", "PATCH", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	prom := NewPrometheusMiddleware()
	s.app.Use(prom.Handler)
	//otel := NewOpenTelemetryMiddleware()
	//s.app.Use(otel)
	//httpLogger := NewLoggingMiddleware(s.logger)
	//s.app.Use(httpLogger.Handler)
	//s.app.Use(versionMiddleware)
}

func (s *Server) startMetricsServer() {
	if s.config.PortMetrics > 0 {
		mux := http.DefaultServeMux
		mux.Handle("/metrics", promhttp.Handler())
		mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, err := w.Write([]byte("OK"))
			if err != nil {
				return
			}
		})

		srv := &http.Server{
			Addr:    fmt.Sprintf(":%v", s.config.PortMetrics),
			Handler: mux,
		}

		err := srv.ListenAndServe()
		if err != nil {
			return
		}
	}
}

// BaseResponse, tüm API yanıtları için temel yapıyı tanımlar
type BaseResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}
