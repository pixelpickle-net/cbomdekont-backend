package http

import (
	"fmt"
	"github.com/gomodule/redigo/redis"
	"github.com/mehmetsafabenli/cbomdekont/pkg/version"
	"go.uber.org/zap"
	"net/url"
	"time"
)

func (s *Server) getCacheConn() (redis.Conn, error) {
	redisUrl, err := url.Parse(s.config.CacheServer)
	if err != nil {
		return nil, fmt.Errorf("failed to parse redis url: %v", err)
	}

	var opts []redis.DialOption
	if user := redisUrl.User; user != nil {
		opts = append(opts, redis.DialUsername(user.Username()))
		if password, ok := user.Password(); ok {
			opts = append(opts, redis.DialPassword(password))
		}
	}

	return redis.Dial("tcp", redisUrl.Host, opts...)
}

func (s *Server) startCachePool(ticker *time.Ticker) {
	if s.config.CacheServer == "" {
		return
	}
	s.pool = &redis.Pool{
		MaxIdle:     3,
		IdleTimeout: 240 * time.Second,
		Dial:        s.getCacheConn,
		TestOnBorrow: func(c redis.Conn, t time.Time) error {
			_, err := c.Do("PING")
			return err
		},
	}

	// set <hostname>=<version> with an expiry time of one minute
	setVersion := func() {
		conn := s.pool.Get()
		if _, err := conn.Do("SET", s.config.Hostname, version.VERSION, "EX", 60); err != nil {
			s.logger.Warn("cache server is offline", zap.Error(err), zap.String("server", s.config.CacheServer))
		}
		_ = conn.Close()
	}

	// set version on a schedule
	go func() {
		setVersion()
		for {
			select {
			case <-ticker.C:
				setVersion()
			}
		}
	}()
}
