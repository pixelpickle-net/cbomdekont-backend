package signals

import (
	"context"
	"github.com/gofiber/fiber/v3"
	"github.com/gomodule/redigo/redis"
	"github.com/spf13/viper"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.uber.org/zap"
	"sync/atomic"
	"time"
)

type Shutdown struct {
	logger                *zap.Logger
	pool                  *redis.Pool
	tracerProvider        *sdktrace.TracerProvider
	serverShutdownTimeout time.Duration
}

func NewShutdown(serverShutdownTimeout time.Duration, logger *zap.Logger) (*Shutdown, error) {
	srv := &Shutdown{
		logger:                logger,
		serverShutdownTimeout: serverShutdownTimeout,
	}

	return srv, nil
}

func (s *Shutdown) Graceful(stopCh <-chan struct{}, httpServer *fiber.App, healthy *int32, ready *int32) {
	ctx := context.Background()

	<-stopCh
	ctx, cancel := context.WithTimeout(ctx, s.serverShutdownTimeout)
	defer cancel()

	atomic.StoreInt32(healthy, 0)
	atomic.StoreInt32(ready, 0)

	if s.pool != nil {
		_ = s.pool.Close()
	}

	//we are waiting 3 second because logger may not be able to log the shutdown message
	s.logger.Info("Shutting down HTTP/HTTPS server.go", zap.Duration("timeout", s.serverShutdownTimeout))
	if viper.GetString("level") != "debug" {
		time.Sleep(3 * time.Second)
	}

	// stop OpenTelemetry tracer provider
	if s.tracerProvider != nil {
		if err := s.tracerProvider.Shutdown(ctx); err != nil {
			s.logger.Warn("stopping tracer provider", zap.Error(err))
		}
	}
	
	// determine if the http server.go was started
	if httpServer != nil {
		if err := httpServer.ShutdownWithContext(ctx); err != nil {
			s.logger.Warn("HTTP server.go graceful shutdown failed", zap.Error(err))
		}
	}
}
