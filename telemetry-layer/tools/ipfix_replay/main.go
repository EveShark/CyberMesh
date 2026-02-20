package main

import (
	"encoding/base64"
	"encoding/hex"
	"flag"
	"fmt"
	"net"
	"os"
	"strings"
	"time"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:2055", "UDP address of IPFIX listener")
	input := flag.String("input", "", "Path to IPFIX payload (raw, hex, or base64)")
	format := flag.String("format", "raw", "raw|hex|base64")
	count := flag.Int("count", 1, "How many times to send")
	interval := flag.Int("interval-ms", 250, "Delay between sends (ms)")
	flag.Parse()

	if strings.TrimSpace(*input) == "" {
		fmt.Fprintln(os.Stderr, "input required")
		os.Exit(2)
	}
	payload, err := loadPayload(*input, *format)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if len(payload) == 0 {
		fmt.Fprintln(os.Stderr, "empty payload")
		os.Exit(2)
	}

	conn, err := net.Dial("udp", *addr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	defer conn.Close()

	delay := time.Duration(*interval) * time.Millisecond
	for i := 0; i < *count; i++ {
		if _, err := conn.Write(payload); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		time.Sleep(delay)
	}
	fmt.Printf("sent %d packets to %s\n", *count, *addr)
}

func loadPayload(path, format string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "raw":
		return raw, nil
	case "hex":
		clean := strings.ReplaceAll(string(raw), " ", "")
		clean = strings.ReplaceAll(clean, "\n", "")
		return hex.DecodeString(clean)
	case "base64":
		clean := strings.TrimSpace(string(raw))
		return base64.StdEncoding.DecodeString(clean)
	default:
		return nil, fmt.Errorf("unsupported format: %s", format)
	}
}
