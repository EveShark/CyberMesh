package capture

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"

	telemetrypb "cybermesh/telemetry-layer/proto/gen/go"
)

type Config struct {
	Mode              string
	FilePath          string
	DefaultSize       int64
	TcpdumpPath       string
	Interface         string
	Snaplen           int
	Promisc           bool
	LibpcapTimeoutSec int
}

type Result struct {
	Reader      io.ReadCloser
	Size        int64
	ContentType string
	Cleanup     func() error
}

func Capture(ctx context.Context, cfg Config, req *telemetrypb.PcapRequestV1, maxBytes int64) (Result, error) {
	_ = ctx
	mode := strings.ToLower(strings.TrimSpace(cfg.Mode))
	if mode == "" {
		mode = "mock"
	}
	if maxBytes <= 0 {
		maxBytes = cfg.DefaultSize
	}
	switch mode {
	case "file":
		return captureFromFile(cfg.FilePath, maxBytes)
	case "tcpdump":
		return captureTcpdump(ctx, cfg, req, maxBytes)
	case "libpcap":
		return captureLibpcap(ctx, cfg, req, maxBytes)
	case "mock":
		return captureMock(maxBytes)
	default:
		return Result{}, errors.New("unsupported capture mode")
	}
}

func captureFromFile(path string, maxBytes int64) (Result, error) {
	if strings.TrimSpace(path) == "" {
		return Result{}, errors.New("PCAP_CAPTURE_FILE required")
	}
	file, err := os.Open(path)
	if err != nil {
		return Result{}, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return Result{}, err
	}
	size := info.Size()
	if maxBytes > 0 && size > maxBytes {
		size = maxBytes
	}
	reader := io.NopCloser(io.LimitReader(file, size))
	return Result{
		Reader:      reader,
		Size:        size,
		ContentType: "application/vnd.tcpdump.pcap",
		Cleanup:     file.Close,
	}, nil
}

func captureMock(maxBytes int64) (Result, error) {
	if maxBytes <= 0 {
		return Result{}, errors.New("maxBytes required")
	}
	payload := mockPcap(maxBytes)
	return Result{
		Reader:      io.NopCloser(bytes.NewReader(payload)),
		Size:        int64(len(payload)),
		ContentType: "application/vnd.tcpdump.pcap",
		Cleanup:     func() error { return nil },
	}, nil
}

func mockPcap(maxBytes int64) []byte {
	header := []byte{
		0xd4, 0xc3, 0xb2, 0xa1,
		0x02, 0x00,
		0x04, 0x00,
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
		0xff, 0xff, 0x00, 0x00,
		0x01, 0x00, 0x00, 0x00,
	}
	if maxBytes <= int64(len(header)) {
		return header[:maxBytes]
	}
	out := make([]byte, maxBytes)
	copy(out, header)
	return out
}
