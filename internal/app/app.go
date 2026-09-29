// Package app controls the application lifecycle.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"
)

const (
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 15 * time.Second
	writeTimeout      = 15 * time.Second
	idleTimeout       = 60 * time.Second
	shutdownTimeout   = 10 * time.Second
)

// Run starts the HTTP server and the background tasks and blocks until all of them stop.
// Canceling ctx or a failing background task shuts everything down.
func Run(ctx context.Context, address string, handler http.Handler, logger *slog.Logger, background ...func(context.Context) error) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var tasks sync.WaitGroup
	taskErrors := make(chan error, len(background))
	for _, task := range background {
		tasks.Go(func() {
			if err := task(ctx); err != nil {
				taskErrors <- err
				cancel()
			}
		})
	}

	listenConfig := &net.ListenConfig{}
	serveErr := runWithListener(ctx, address, handler, logger, listenConfig.Listen)
	cancel()
	tasks.Wait()
	close(taskErrors)

	errs := []error{serveErr}
	for err := range taskErrors {
		errs = append(errs, err)
	}

	return errors.Join(errs...)
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
	logger.InfoContext(ctx, "HTTP server listening", "address", listener.Addr().String())
	serveError := make(chan error, 1)
	go func() {
		serveError <- server.Serve(listener)
	}()

	select {
	case err := <-serveError:
		return normalizeServeError(err)
	case <-ctx.Done():
	}

	logger.InfoContext(ctx, "shutting down HTTP server")
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
	logger.InfoContext(ctx, "HTTP server stopped")

	return normalizeServeError(<-serveError)
}

func normalizeServeError(err error) error {
	if err == nil || errors.Is(err, http.ErrServerClosed) {
		return nil
	}

	return fmt.Errorf("serve HTTP: %w", err)
}
