package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mehmetsafabenli/cbomdekont/pkg/api/http"
	"github.com/mehmetsafabenli/cbomdekont/pkg/signals"
	"github.com/prometheus/common/version"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// BaseResponse, tüm API yanıtları için temel yapıyı tanımlar
type BaseResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

func init() {
	println("eminin götü  kocaman keza denizinkide öyle")
}

func main() {
	fs := pflag.NewFlagSet("default", pflag.ContinueOnError)
	fs.String("config", "config.yaml", "path to config file")
	fs.String("config-path", ".", "config file directory")
	fs.String("port", "9898", "port to bind HTTP listener")
	fs.String("level", "info", "log level debug, info, warn, error, fatal or panic")

	versionFlag := fs.BoolP("version", "v", false, "version number")

	err := fs.Parse(os.Args[1:])
	switch {
	case errors.Is(err, pflag.ErrHelp):
		os.Exit(0)
	case err != nil:
		_, err := fmt.Fprintf(os.Stderr, "Error: %s\n\n", err.Error())
		if err != nil {
			return
		}
		fs.PrintDefaults()
		os.Exit(2)
	case *versionFlag:
		fmt.Println(version.Version)
		os.Exit(0)
	}

	err = viper.BindPFlags(fs)
	if err != nil {
		panic(err)
	}
	hostname, _ := os.Hostname()
	viper.Set("hostname", hostname)
	viper.SetEnvPrefix("EVENT")
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.AutomaticEnv()

	if _, err := os.Stat(filepath.Join(viper.GetString("config-path"), viper.GetString("config"))); err == nil {
		viper.SetConfigName(strings.TrimSuffix(viper.GetString("config"), filepath.Ext(viper.GetString("config"))))
		viper.AddConfigPath(viper.GetString("config-path"))
		err = viper.ReadInConfig()
		if err != nil {
			panic(err)
		}
	}

	logger, err := configureLogging("info")
	defer logger.Sync()
	if err != nil {
		logger.Fatal("failed to sync logger", zap.Error(err))
		return
	}
	stdLog := zap.RedirectStdLog(logger)
	defer stdLog()

	logger.Info("Starting application", zap.String("version", viper.GetString("version")))

	var srvCfg http.Config
	if err := viper.Unmarshal(&srvCfg); err != nil {
		logger.Panic("config unmarshal failed", zap.Error(err))
	}

	var awsCfg http.AWSConfig
	if err := viper.UnmarshalKey("aws", &awsCfg); err != nil {
		logger.Panic("AWS config unmarshal failed", zap.Error(err))
	}

	// schema.json dosyasının yolunu doğru şekilde belirtin
	schemaPath := filepath.Join(viper.GetString("config-path"), "schema.json")
	awsServer, err := http.NewAWSService(logger, &awsCfg, schemaPath)
	if err != nil {
		logger.Panic("Failed to initialize AWS service", zap.Error(err))
	}

	logger.Info("Starting HTTP server", zap.String("port", srvCfg.Port))

	//start http server
	srv, _ := http.NewServer(&srvCfg, logger, awsServer)

	httpServer, healthy, ready := srv.ListenAndServe()

	//graceful shutdown
	stopCh := signals.SetupSignalHandler()
	sd, _ := signals.NewShutdown(srvCfg.ServerShutdownTimeout, logger)
	sd.Graceful(stopCh, httpServer, healthy, ready)

}

func configureLogging(logLevel string) (*zap.Logger, error) {
	level := zap.NewAtomicLevelAt(zapcore.InfoLevel)
	switch logLevel {
	case "debug":
		level = zap.NewAtomicLevelAt(zapcore.DebugLevel)
	case "info":
		level = zap.NewAtomicLevelAt(zapcore.InfoLevel)
	case "warn":
		level = zap.NewAtomicLevelAt(zapcore.WarnLevel)
	case "error":
		level = zap.NewAtomicLevelAt(zapcore.ErrorLevel)
	case "fatal":
		level = zap.NewAtomicLevelAt(zapcore.FatalLevel)
	case "panic":
		level = zap.NewAtomicLevelAt(zapcore.PanicLevel)
	}

	zapEncoderConfig := zapcore.EncoderConfig{
		TimeKey:        "ts",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		MessageKey:     "msg",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.LowercaseLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.SecondsDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}

	zapConfig := zap.Config{
		Level:       level,
		Development: false,
		Sampling: &zap.SamplingConfig{
			Initial:    100,
			Thereafter: 100,
		},
		Encoding:         "json",
		EncoderConfig:    zapEncoderConfig,
		OutputPaths:      []string{"stderr"},
		ErrorOutputPaths: []string{"stderr"},
	}

	return zapConfig.Build()
}
