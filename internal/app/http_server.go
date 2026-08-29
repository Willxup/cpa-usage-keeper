package app

import (
	"net/http"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/logging"
	"github.com/sirupsen/logrus"
)

const (
	httpReadHeaderTimeout = 5 * time.Second
	httpIdleTimeout       = 60 * time.Second
	httpMaxHeaderBytes    = 64 << 10
)

func NewHTTPServer(cfg config.Config, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              cfg.ListenAddress(),
		Handler:           handler,
		ErrorLog:          logging.NewStandardLogger(logrus.ErrorLevel),
		ReadHeaderTimeout: httpReadHeaderTimeout,
		IdleTimeout:       httpIdleTimeout,
		MaxHeaderBytes:    httpMaxHeaderBytes,
	}
}
