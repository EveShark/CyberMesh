//go:build pcap

package capture

import (
	"context"
	"errors"
	"io"
	"os"
	"time"

	telemetrypb "cybermesh/telemetry-layer/proto/gen/go"
	"github.com/google/gopacket"
	"github.com/google/gopacket/pcap"
	"github.com/google/gopacket/pcapgo"
)

func captureLibpcap(ctx context.Context, cfg Config, req *telemetrypb.PcapRequestV1, maxBytes int64) (Result, error) {
	iface := cfg.Interface
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
	duration := time.Duration(req.DurationMs) * time.Millisecond
	if duration <= 0 {
		return Result{}, errors.New("duration_ms required")
	}

	handle, err := pcap.OpenLive(iface, int32(cfg.Snaplen), cfg.Promisc, time.Second)
	if err != nil {
		return Result{}, err
	}
	if err := handle.SetBPFFilter(bpf); err != nil {
		handle.Close()
		return Result{}, err
	}

	tmpFile, err := os.CreateTemp("", "pcap-*.pcap")
	if err != nil {
		handle.Close()
		return Result{}, err
	}
	writer := pcapgo.NewWriter(tmpFile)
	if err := writer.WriteFileHeader(uint32(cfg.Snaplen), handle.LinkType()); err != nil {
		_ = tmpFile.Close()
		handle.Close()
		return Result{}, err
	}

	packetSource := gopacket.NewPacketSource(handle, handle.LinkType())
	deadline := time.Now().Add(duration)
	var bytesWritten int64

	for {
		if ctx.Err() != nil || time.Now().After(deadline) {
			break
		}
		packet, err := packetSource.NextPacket()
		if err != nil {
			continue
		}
		if err := writer.WritePacket(packet.Metadata().CaptureInfo, packet.Data()); err != nil {
			break
		}
		bytesWritten += int64(len(packet.Data()))
		if maxBytes > 0 && bytesWritten >= maxBytes {
			break
		}
	}

	_ = tmpFile.Sync()
	_ = tmpFile.Close()
	handle.Close()

	file, err := os.Open(tmpFile.Name())
	if err != nil {
		_ = os.Remove(tmpFile.Name())
		return Result{}, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		_ = os.Remove(tmpFile.Name())
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
			return os.Remove(tmpFile.Name())
		},
	}, nil
}
