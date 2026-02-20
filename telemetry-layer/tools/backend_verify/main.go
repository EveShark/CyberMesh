package main

import (
	"encoding/hex"
	"flag"
	"fmt"
	"os"

	"backend/pkg/ingest/kafka"
	"backend/pkg/utils"
)

func main() {
	var (
		hexPayload = flag.String("hex", "", "hex-encoded ai.anomalies.v1 protobuf payload")
		filePath   = flag.String("file", "", "path to file containing raw protobuf bytes")
	)
	flag.Parse()

	var raw []byte
	switch {
	case *hexPayload != "":
		b, err := hex.DecodeString(*hexPayload)
		if err != nil {
			fmt.Fprintf(os.Stderr, "decode hex: %v\n", err)
			os.Exit(2)
		}
		raw = b
	case *filePath != "":
		b, err := os.ReadFile(*filePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "read file: %v\n", err)
			os.Exit(2)
		}
		raw = b
	default:
		fmt.Fprintln(os.Stderr, "provide -hex or -file")
		os.Exit(2)
	}

	msg, err := kafka.DecodeAnomalyMsg(raw)
	if err != nil {
		fmt.Fprintf(os.Stderr, "DecodeAnomalyMsg failed: %v\n", err)
		os.Exit(1)
	}

	// Optional logger; backend verifier logs are verbose, keep nil by default.
	var log *utils.Logger = nil
	_, err = kafka.VerifyAnomalyMsg(msg, kafka.DefaultVerifierConfig(), log)
	if err != nil {
		fmt.Fprintf(os.Stderr, "VerifyAnomalyMsg failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("OK id=%s type=%s source=%s severity=%d confidence=%.3f ts=%d model=%s\n",
		msg.ID, msg.Type, msg.Source, msg.Severity, msg.Confidence, msg.TS, msg.ModelVersion)
}

