package server_test

import (
	"appointment-manager/internal/server"
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	loopbackAddress   = "127.0.0.1:0"
	serverAddress     = "127.0.0.1:8080"
	startWaitTimeout  = 2 * time.Second
	shutdownWaitLimit = 3 * time.Second
	testReadTimeout   = 200 * time.Millisecond
	testWriteTimeout  = 200 * time.Millisecond
	testIdleTimeout   = 300 * time.Millisecond
	testShutdownWait  = 300 * time.Millisecond
	testMaxHeaderSize = 1024
)

func TestStartWithConfigValidation(t *testing.T) {
	t.Parallel()

	logger := newTestLogger()
	handler := http.NewServeMux()

	tests := []struct {
		name     string
		ctx      context.Context
		logger   *slog.Logger
		handler  http.Handler
		addr     string
		expected error
	}{
		{name: "nil context", ctx: nil, logger: logger, handler: handler, addr: serverAddress, expected: server.ErrNilContext},
		{name: "empty address", ctx: context.Background(), logger: logger, handler: handler, addr: "", expected: server.ErrEmptyAddress},
		{name: "nil logger", ctx: context.Background(), logger: nil, handler: handler, addr: serverAddress, expected: server.ErrNilLogger},
		{name: "nil handler", ctx: context.Background(), logger: logger, handler: nil, addr: serverAddress, expected: server.ErrNilHandler},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := server.Start(tt.ctx, tt.logger, tt.handler, tt.addr, validConfig())

			require.Error(t, err)
			assert.True(t, errors.Is(err, tt.expected))
		})
	}
}

func TestStartValidationRequiresAddress(t *testing.T) {
	t.Parallel()

	err := server.Start(context.Background(), newTestLogger(), http.NewServeMux(), "", validConfig())

	require.Error(t, err)
	assert.True(t, errors.Is(err, server.ErrEmptyAddress))
}

func TestStartValidationRequiresValidConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		cfg      server.Config
		contains string
		expected error
	}{
		{name: "invalid read header timeout", cfg: withConfig(func(cfg *server.Config) { cfg.ReadHeaderTimeout = 0 }), contains: "invalid read header timeout", expected: server.ErrInvalidConfig},
		{name: "invalid read timeout", cfg: withConfig(func(cfg *server.Config) { cfg.ReadTimeout = 0 }), contains: "invalid read timeout", expected: server.ErrInvalidConfig},
		{name: "invalid write timeout", cfg: withConfig(func(cfg *server.Config) { cfg.WriteTimeout = 0 }), contains: "invalid write timeout", expected: server.ErrInvalidConfig},
		{name: "invalid idle timeout", cfg: withConfig(func(cfg *server.Config) { cfg.IdleTimeout = 0 }), contains: "invalid idle timeout", expected: server.ErrInvalidConfig},
		{name: "invalid shutdown timeout", cfg: withConfig(func(cfg *server.Config) { cfg.ShutdownTimeout = 0 }), contains: "invalid shutdown timeout", expected: server.ErrInvalidConfig},
		{name: "invalid max header bytes", cfg: withConfig(func(cfg *server.Config) { cfg.MaxHeaderBytes = 0 }), contains: "invalid max header bytes", expected: server.ErrInvalidConfig},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := server.Start(context.Background(), newTestLogger(), http.NewServeMux(), serverAddress, tt.cfg)

			require.Error(t, err)
			assert.True(t, errors.Is(err, tt.expected))
			assert.ErrorContains(t, err, tt.contains)
		})
	}
}

func TestStartWithConfigReturnsListenError(t *testing.T) {
	t.Parallel()

	listenerConfig := net.ListenConfig{}
	occupiedListener, err := listenerConfig.Listen(context.Background(), "tcp", loopbackAddress)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, occupiedListener.Close())
	})

	err = server.Start(
		context.Background(),
		newTestLogger(),
		http.NewServeMux(),
		occupiedListener.Addr().String(),
		validConfig(),
	)

	require.Error(t, err)
	assert.ErrorContains(t, err, "listen and serve")
}

func TestStartWithConfigShutsDownOnContextCancel(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	addr := reserveAddress(t)
	errCh := make(chan error, 1)

	go func() {
		errCh <- server.Start(ctx, newTestLogger(), http.NewServeMux(), addr, validConfig())
	}()

	dialer := &net.Dialer{Timeout: 150 * time.Millisecond}
	require.Eventually(t, func() bool {
		conn, err := dialer.DialContext(context.Background(), "tcp", addr)
		if err != nil {
			return false
		}
		defer func() {
			_ = conn.Close()
		}()

		return true
	}, startWaitTimeout, 20*time.Millisecond)

	cancel()

	select {
	case err := <-errCh:
		require.NoError(t, err)
	case <-time.After(shutdownWaitLimit):
		t.Fatal("server did not shutdown in time")
	}
}

func reserveAddress(t *testing.T) string {
	t.Helper()

	listenerConfig := net.ListenConfig{}
	listener, err := listenerConfig.Listen(context.Background(), "tcp", loopbackAddress)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, listener.Close())
	}()

	return listener.Addr().String()
}

func newTestLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func validConfig() server.Config {
	return server.Config{
		ReadHeaderTimeout: testReadTimeout,
		ReadTimeout:       testReadTimeout,
		WriteTimeout:      testWriteTimeout,
		IdleTimeout:       testIdleTimeout,
		ShutdownTimeout:   testShutdownWait,
		MaxHeaderBytes:    testMaxHeaderSize,
	}
}

func withConfig(update func(cfg *server.Config)) server.Config {
	cfg := validConfig()
	update(&cfg)
	return cfg
}
