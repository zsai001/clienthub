package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/cltx/clienthub/config"
	"github.com/cltx/clienthub/pkg/client"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func main() {
	cfgPath := flag.String("config", "client.yaml", "path to client config file")
	flag.Parse()

	cfg, err := config.LoadClientConfig(*cfgPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	logger := newLogger(cfg.LogLevel)
	defer logger.Sync()

	c := client.New(cfg, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		logger.Info("received signal, shutting down", zap.String("signal", sig.String()))
		cancel()
	}()

	logger.Info("starting clienthub client",
		zap.String("name", cfg.ClientName),
		zap.String("server", cfg.ServerAddr))

	if err := c.Run(ctx); err != nil && err != context.Canceled {
		logger.Fatal("client error", zap.Error(err))
	}
}

func newLogger(level string) *zap.Logger {
	var lvl zapcore.Level
	switch level {
	case "debug":
		lvl = zapcore.DebugLevel
	case "warn":
		lvl = zapcore.WarnLevel
	case "error":
		lvl = zapcore.ErrorLevel
	default:
		lvl = zapcore.InfoLevel
	}

	cfg := zap.Config{
		Level:            zap.NewAtomicLevelAt(lvl),
		Encoding:         "console",
		EncoderConfig:    zap.NewDevelopmentEncoderConfig(),
		OutputPaths:      []string{"stdout"},
		ErrorOutputPaths: []string{"stderr"},
	}
	logger, err := cfg.Build()
	if err != nil {
		log.Fatalf("init logger: %v", err)
	}
	return logger
}
