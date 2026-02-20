package main

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"flag"
	"fmt"
	"net"
	"os"
	"strings"
	"time"
)

type ipfixField struct {
	id     uint16
	length uint16
}

func main() {
	var (
		srcIP         = flag.String("src-ip", "10.0.0.10", "source IPv4 address")
		dstIP         = flag.String("dst-ip", "10.0.0.20", "destination IPv4 address")
		srcPort       = flag.Uint("src-port", 51512, "source port")
		dstPort       = flag.Uint("dst-port", 443, "destination port")
		proto         = flag.Uint("protocol", 6, "protocol number (TCP=6, UDP=17)")
		packets       = flag.Uint64("packets", 1, "packet delta count")
		bytesCount    = flag.Uint64("bytes", 0, "octet delta count")
		startMS       = flag.Int64("start-ms", 0, "flow start ms since epoch (0=now)")
		endMS         = flag.Int64("end-ms", 0, "flow end ms since epoch (0=now+5ms)")
		templateID    = flag.Uint("template-id", 256, "IPFIX template ID")
		obsDomainID   = flag.Uint("obs-domain-id", 1, "observation domain ID")
		sequence      = flag.Uint("sequence", 1, "sequence number")
		includeTpl    = flag.Bool("include-template", true, "include template set")
		includeData   = flag.Bool("include-data", true, "include data set")
		format        = flag.String("format", "raw", "output format: raw|hex|base64")
		output        = flag.String("output", "", "output file (optional)")
		udpAddr       = flag.String("udp", "", "send via UDP to host:port (optional)")
		repeat        = flag.Int("count", 1, "send count when using --udp")
		splitPackets  = flag.Bool("split", false, "send template/data as separate UDP packets (same socket)")
	)
	flag.Parse()

	if !*includeTpl && !*includeData {
		fatal("at least one of --include-template or --include-data must be true")
	}

	src := net.ParseIP(*srcIP).To4()
	dst := net.ParseIP(*dstIP).To4()
	if src == nil || dst == nil {
		fatal("only IPv4 addresses are supported")
	}

	now := time.Now().UTC().UnixMilli()
	start := *startMS
	if start == 0 {
		start = now
	}
	end := *endMS
	if end == 0 {
		end = start + 5
	}
	if end < start {
		end = start
	}

	fields := []ipfixField{
		{id: 8, length: 4},   // sourceIPv4Address
		{id: 12, length: 4},  // destinationIPv4Address
		{id: 7, length: 2},   // sourceTransportPort
		{id: 11, length: 2},  // destinationTransportPort
		{id: 4, length: 1},   // protocolIdentifier
		{id: 2, length: 8},   // packetDeltaCount
		{id: 1, length: 8},   // octetDeltaCount
		{id: 152, length: 8}, // flowStartMilliseconds
		{id: 153, length: 8}, // flowEndMilliseconds
	}

	var sets [][]byte
	templateSet := []byte{}
	dataSet := []byte{}
	if *includeTpl {
		templateSet = buildTemplateSet(uint16(*templateID), fields)
		sets = append(sets, templateSet)
	}
	if *includeData {
		dataSet = buildDataSet(uint16(*templateID), src, dst, uint16(*srcPort), uint16(*dstPort), uint8(*proto), *packets, *bytesCount, uint64(start), uint64(end), fields)
		sets = append(sets, dataSet)
	}

	message := buildMessage(uint32(*sequence), uint32(*obsDomainID), sets)
	payload, err := encodePayload(message, *format)
	if err != nil {
		fatal(err.Error())
	}

	if *output != "" {
		if err := os.WriteFile(*output, payload, 0o644); err != nil {
			fatal(err.Error())
		}
	}

	if *udpAddr != "" {
		if *splitPackets && *includeTpl && *includeData {
			if err := sendSplitUDP(*udpAddr, templateSet, dataSet, uint32(*sequence), uint32(*obsDomainID), *repeat); err != nil {
				fatal(err.Error())
			}
		} else {
			if err := sendUDP(*udpAddr, message, *repeat); err != nil {
				fatal(err.Error())
			}
		}
	} else {
		if *output == "" {
			os.Stdout.Write(payload)
		}
	}
}

func buildMessage(sequence, domain uint32, sets [][]byte) []byte {
	var body bytes.Buffer
	for _, set := range sets {
		body.Write(set)
	}

	length := 16 + body.Len()
	buf := bytes.NewBuffer(make([]byte, 0, length))
	writeU16(buf, 10)                     // version
	writeU16(buf, uint16(length))         // length
	writeU32(buf, uint32(time.Now().Unix()))
	writeU32(buf, sequence)
	writeU32(buf, domain)
	buf.Write(body.Bytes())
	return buf.Bytes()
}

func buildTemplateSet(templateID uint16, fields []ipfixField) []byte {
	var record bytes.Buffer
	writeU16(&record, templateID)
	writeU16(&record, uint16(len(fields)))
	for _, f := range fields {
		writeU16(&record, f.id)
		writeU16(&record, f.length)
	}
	setLength := 4 + record.Len()
	buf := bytes.NewBuffer(make([]byte, 0, setLength))
	writeU16(buf, 2) // set ID for template
	writeU16(buf, uint16(setLength))
	buf.Write(record.Bytes())
	return buf.Bytes()
}

func buildDataSet(templateID uint16, src, dst net.IP, srcPort, dstPort uint16, proto uint8, packets, bytesCount uint64, start, end uint64, fields []ipfixField) []byte {
	var record bytes.Buffer
	record.Write(src)
	record.Write(dst)
	writeU16(&record, srcPort)
	writeU16(&record, dstPort)
	record.WriteByte(proto)
	writeU64(&record, packets)
	writeU64(&record, bytesCount)
	writeU64(&record, start)
	writeU64(&record, end)

	setLength := 4 + record.Len()
	padding := (4 - (setLength % 4)) % 4
	for i := 0; i < padding; i++ {
		record.WriteByte(0)
	}
	setLength = 4 + record.Len()

	buf := bytes.NewBuffer(make([]byte, 0, setLength))
	writeU16(buf, templateID)
	writeU16(buf, uint16(setLength))
	buf.Write(record.Bytes())
	return buf.Bytes()
}

func encodePayload(raw []byte, format string) ([]byte, error) {
	switch strings.ToLower(format) {
	case "raw":
		return raw, nil
	case "hex":
		encoded := hex.EncodeToString(raw)
		return []byte(encoded), nil
	case "base64", "b64":
		encoded := base64.StdEncoding.EncodeToString(raw)
		return []byte(encoded), nil
	default:
		return nil, fmt.Errorf("unsupported format %q", format)
	}
}

func sendUDP(addr string, payload []byte, count int) error {
	if count <= 0 {
		count = 1
	}
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return err
	}
	conn, err := net.DialUDP("udp", nil, udpAddr)
	if err != nil {
		return err
	}
	defer conn.Close()

	for i := 0; i < count; i++ {
		if _, err := conn.Write(payload); err != nil {
			return err
		}
	}
	return nil
}

func sendSplitUDP(addr string, templateSet, dataSet []byte, sequence, domain uint32, count int) error {
	if count <= 0 {
		count = 1
	}
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return err
	}
	conn, err := net.DialUDP("udp", nil, udpAddr)
	if err != nil {
		return err
	}
	defer conn.Close()

	for i := 0; i < count; i++ {
		if len(templateSet) > 0 {
			msg := buildMessage(sequence, domain, [][]byte{templateSet})
			if _, err := conn.Write(msg); err != nil {
				return err
			}
		}
		if len(dataSet) > 0 {
			msg := buildMessage(sequence, domain, [][]byte{dataSet})
			if _, err := conn.Write(msg); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeU16(buf *bytes.Buffer, value uint16) {
	_ = binary.Write(buf, binary.BigEndian, value)
}

func writeU32(buf *bytes.Buffer, value uint32) {
	_ = binary.Write(buf, binary.BigEndian, value)
}

func writeU64(buf *bytes.Buffer, value uint64) {
	_ = binary.Write(buf, binary.BigEndian, value)
}

func fatal(msg string) {
	fmt.Fprintln(os.Stderr, msg)
	os.Exit(1)
}
