// Package app controls the application lifecycle.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"
)

const (
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 15 * time.Second
	writeTimeout      = 15 * time.Second
	idleTimeout       = 60 * time.Second
	shutdownTimeout   = 10 * time.Second
)

// Run starts the HTTP server and blocks until it stops.
func Run(ctx context.Context, address string, handler http.Handler, logger *slog.Logger) error {
	listenConfig := &net.ListenConfig{}

	return runWithListener(ctx, address, handler, logger, listenConfig.Listen)
}

func runWithListener(
	ctx context.Context,
	address string,
	handler http.Handler,
	logger *slog.Logger,
	listen func(context.Context, string, string) (net.Listener, error),
) error {
	select {
	case <-ctx.Done():
		return nil
	default:
	}

	listener, err := listen(ctx, "tcp", address)
	if err != nil {
		return normalizeListenError(ctx)
	}

	return serve(ctx, listener, handler, logger)
}

func normalizeListenError(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return nil
	default:
		return errors.New("listen: unable to bind configured address")
	}
}

func serve(ctx context.Context, listener net.Listener, handler http.Handler, logger *slog.Logger) error {
	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}
	serveError := make(chan error, 1)
	go func() {
		serveError <- server.Serve(listener)
	}()

	select {
	case err := <-serveError:
		return normalizeServeError(err)
	case <-ctx.Done():
	}

	logger.Info("shutting down HTTP server")
	shutdownContext, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownContext); err != nil {
		if closeErr := server.Close(); closeErr != nil {
			return errors.Join(
				fmt.Errorf("shut down HTTP server: %w", err),
				fmt.Errorf("close HTTP server: %w", closeErr),
			)
		}

		return fmt.Errorf("shut down HTTP server: %w", err)
	}

	return normalizeServeError(<-serveError)
}

func normalizeServeError(err error) error {
	if err == nil || errors.Is(err, http.ErrServerClosed) {
		return nil
	}

	return fmt.Errorf("serve HTTP: %w", err)
}
