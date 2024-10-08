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

func main() {
	fs := pflag.NewFlagSet("default", pflag.ContinueOnError)
	fs.String("config", "config.yaml", "path to config file")
	fs.String("config-path", ".", "config file directory")
	fs.String("port", "80", "port to bind HTTP listener")
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

	configPath := viper.GetString("config-path")
	configFile := viper.GetString("config")

	if _, err := os.Stat(filepath.Join(configPath, configFile)); err == nil {
		viper.SetConfigName(strings.TrimSuffix(configFile, filepath.Ext(configFile)))
		viper.AddConfigPath(configPath)
		err = viper.ReadInConfig()
		if err != nil {
			fmt.Println("Config file not found, using default values")
		}
	} else {
		fmt.Println("Config file not found, using default values")
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
	awsCfg.AccessKeyID = os.Getenv("AWS_ACCESS_KEY_ID")
	awsCfg.SecretAccessKey = os.Getenv("AWS_SECRET_ACCESS_KEY")
	awsCfg.Region = os.Getenv("AWS_REGION")

	if awsCfg.AccessKeyID == "" || awsCfg.SecretAccessKey == "" || awsCfg.Region == "" {
		logger.Panic("AWS credentials are not set properly")
	}

	// schema.json dosyasının yolunu doğru şekilde belirtin
	schemaPath := "/root/schema.json"
	if _, err := os.Stat(schemaPath); os.IsNotExist(err) {
		logger.Panic("schema.json file not found", zap.String("path", schemaPath), zap.Error(err))
	}

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
