package capture

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	telemetrypb "cybermesh/telemetry-layer/proto/gen/go"
)

func captureTcpdump(ctx context.Context, cfg Config, req *telemetrypb.PcapRequestV1, maxBytes int64) (Result, error) {
	tcpdump := strings.TrimSpace(cfg.TcpdumpPath)
	if tcpdump == "" {
		tcpdump = "tcpdump"
	}
	iface := strings.TrimSpace(cfg.Interface)
	if iface == "" {
		iface = "any"
	}
	if cfg.Snaplen <= 0 {
		cfg.Snaplen = 96
	}

	bpf := buildBPF(req)
	if bpf == "" {
		return Result{}, errors.New("bpf filter required")
	}

	tmpFile, err := os.CreateTemp("", "pcap-*.pcap")
	if err != nil {
		return Result{}, err
	}
	tmpPath := tmpFile.Name()
	_ = tmpFile.Close()

	duration := time.Duration(req.DurationMs) * time.Millisecond
	if duration <= 0 {
		return Result{}, errors.New("duration_ms required")
	}

	args := []string{
		"-i", iface,
		"-s", fmt.Sprintf("%d", cfg.Snaplen),
		"-w", tmpPath,
		"-G", fmt.Sprintf("%d", int(duration.Seconds())),
		"-W", "1",
	}
	if !cfg.Promisc {
		args = append(args, "-p")
	}
	args = append(args, bpf)

	cmd := exec.CommandContext(ctx, tcpdump, args...)
	if err := cmd.Run(); err != nil {
		_ = os.Remove(tmpPath)
		return Result{}, err
	}

	file, err := os.Open(tmpPath)
	if err != nil {
		_ = os.Remove(tmpPath)
		return Result{}, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		_ = os.Remove(tmpPath)
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
		Cleanup: func() error {
			_ = file.Close()
			return os.Remove(tmpPath)
		},
	}, nil
}

func buildBPF(req *telemetrypb.PcapRequestV1) string {
	parts := []string{}
	if req.Proto == 6 {
		parts = append(parts, "tcp")
	} else if req.Proto == 17 {
		parts = append(parts, "udp")
	}
	if req.SrcIp != "" {
		parts = append(parts, fmt.Sprintf("src host %s", req.SrcIp))
	}
	if req.DstIp != "" {
		parts = append(parts, fmt.Sprintf("dst host %s", req.DstIp))
	}
	if req.SrcPort > 0 {
		parts = append(parts, fmt.Sprintf("src port %d", req.SrcPort))
	}
	if req.DstPort > 0 {
		parts = append(parts, fmt.Sprintf("dst port %d", req.DstPort))
	}
	return strings.Join(parts, " and ")
}

func tempDir(dir string) string {
	if strings.TrimSpace(dir) == "" {
		return ""
	}
	return filepath.Clean(dir)
}
