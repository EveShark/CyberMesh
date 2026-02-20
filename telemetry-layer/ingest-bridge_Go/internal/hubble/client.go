package hubble

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/cilium/cilium/api/v1/observer"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Config struct {
	Address         string
	TLSEnabled      bool
	TLSCAPath       string
	TLSCertPath     string
	TLSKeyPath      string
	TLSServerName   string
	Follow          bool
	SinceSeconds    int64
	MaxFlowsPerRecv uint64
}

type Client struct {
	conn    *grpc.ClientConn
	observer observer.ObserverClient
}

func Dial(ctx context.Context, cfg Config) (*Client, error) {
	if cfg.Address == "" {
		return nil, errors.New("hubble address is required")
	}
	opts := []grpc.DialOption{grpc.WithBlock()}
	if cfg.TLSEnabled {
		tlsConfig, err := buildTLSConfig(cfg)
		if err != nil {
			return nil, err
		}
		opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)))
	} else {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	conn, err := grpc.DialContext(ctx, cfg.Address, opts...)
	if err != nil {
		return nil, fmt.Errorf("dial hubble: %w", err)
	}

	return &Client{
		conn:    conn,
		observer: observer.NewObserverClient(conn),
	}, nil
}

func (c *Client) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (c *Client) Stream(ctx context.Context, cfg Config) (observer.Observer_GetFlowsClient, error) {
	req := &observer.GetFlowsRequest{
		Follow: cfg.Follow,
	}
	if cfg.MaxFlowsPerRecv > 0 {
		req.Number = cfg.MaxFlowsPerRecv
	}
	if cfg.SinceSeconds > 0 {
		req.Since = timestamppb.New(timeNow().Add(-timeDuration(cfg.SinceSeconds)))
	}
	return c.observer.GetFlows(ctx, req)
}

func buildTLSConfig(cfg Config) (*tls.Config, error) {
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
	}
	if cfg.TLSServerName != "" {
		tlsConfig.ServerName = cfg.TLSServerName
	}
	if cfg.TLSCAPath != "" {
		caPEM, err := os.ReadFile(cfg.TLSCAPath)
		if err != nil {
			return nil, fmt.Errorf("read hubble ca: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caPEM) {
			return nil, errors.New("invalid hubble ca")
		}
		tlsConfig.RootCAs = pool
	}
	if cfg.TLSCertPath != "" || cfg.TLSKeyPath != "" {
		if cfg.TLSCertPath == "" || cfg.TLSKeyPath == "" {
			return nil, errors.New("both hubble cert and key are required")
		}
		cert, err := tls.LoadX509KeyPair(cfg.TLSCertPath, cfg.TLSKeyPath)
		if err != nil {
			return nil, fmt.Errorf("load hubble cert: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}
	return tlsConfig, nil
}

func timeDuration(seconds int64) time.Duration {
	return time.Duration(seconds) * time.Second
}

func timeNow() time.Time {
	return time.Now()
}
