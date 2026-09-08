// Package managementhttp exposes the existing Fast Lane application service
// through an optional, authenticated HTTP control plane. It intentionally does
// not own VPN state or background scheduling.
package managementhttp

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"
)

const minAccessTokenLength = 32

// Config controls the optional management HTTP listener.
type Config struct {
	ListenAddr   string
	AccessToken  string
	Logger       *slog.Logger
	RunExclusive func(context.Context, func(context.Context) error) error
	HealthCheck  func(context.Context) error
}

// Server is the lifecycle wrapper used by the Fast Lane daemon.
type Server struct {
	listener net.Listener
	server   *http.Server
}

// NewServer validates the exposure policy and binds the management listener.
func NewServer(ctx context.Context, config Config, service Service) (*Server, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if service == nil {
		return nil, fmt.Errorf("management service is required")
	}

	loopback, err := validateExposure(config.ListenAddr, config.AccessToken)
	if err != nil {
		return nil, err
	}

	listener, err := net.Listen("tcp", config.ListenAddr)
	if err != nil {
		return nil, fmt.Errorf("listen on management address: %w", err)
	}

	handler, err := NewHandler(ctx, service, HandlerConfig{
		AccessToken:  config.AccessToken,
		LoopbackOnly: loopback,
		Logger:       config.Logger,
		RunExclusive: config.RunExclusive,
		HealthCheck:  config.HealthCheck,
	})
	if err != nil {
		_ = listener.Close()
		return nil, err
	}

	return &Server{
		listener: listener,
		server: &http.Server{
			Handler:           handler,
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       30 * time.Second,
			WriteTimeout:      30 * time.Second,
			IdleTimeout:       60 * time.Second,
			MaxHeaderBytes:    16 << 10,
		},
	}, nil
}

// Addr returns the address selected by the listener.
func (s *Server) Addr() net.Addr {
	return s.listener.Addr()
}

// Run serves requests until the context is cancelled or the listener fails.
func (s *Server) Run(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- s.server.Serve(s.listener)
	}()

	select {
	case err := <-serveErr:
		if err == nil || err == http.ErrServerClosed {
			return nil
		}
		return fmt.Errorf("serve management HTTP: %w", err)
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := s.server.Shutdown(shutdownCtx); err != nil {
			_ = s.server.Close()
			return fmt.Errorf("stop management HTTP: %w", err)
		}
		err := <-serveErr
		if err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("serve management HTTP: %w", err)
		}
		return nil
	}
}

func validateExposure(listenAddr, accessToken string) (bool, error) {
	host, _, err := net.SplitHostPort(strings.TrimSpace(listenAddr))
	if err != nil {
		return false, fmt.Errorf("invalid management listen address: %w", err)
	}

	host = strings.Trim(host, "[]")
	loopback := strings.EqualFold(host, "localhost")
	if host != "" && !loopback {
		ip := net.ParseIP(host)
		if ip == nil {
			return false, fmt.Errorf("management listen host must be localhost or an IP address")
		}
		loopback = ip.IsLoopback()
	}

	token := strings.TrimSpace(accessToken)
	if token != "" && len(token) < minAccessTokenLength {
		return false, fmt.Errorf("management access token must contain at least %d characters", minAccessTokenLength)
	}
	if !loopback && token == "" {
		return false, fmt.Errorf("management access token is required outside loopback")
	}

	return loopback, nil
}
