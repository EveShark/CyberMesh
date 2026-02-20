package codec

import (
	"encoding/json"
	"strings"

	telemetrypb "cybermesh/telemetry-layer/proto/gen/go"
	"google.golang.org/protobuf/proto"
)

func DecodeRequest(payload []byte, encoding string) (*telemetrypb.PcapRequestV1, error) {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "protobuf", "proto", "pb":
		msg := &telemetrypb.PcapRequestV1{}
		if err := proto.Unmarshal(payload, msg); err != nil {
			return nil, err
		}
		return msg, nil
	default:
		var msg telemetrypb.PcapRequestV1
		if err := json.Unmarshal(payload, &msg); err != nil {
			return nil, err
		}
		return &msg, nil
	}
}

func EncodeResult(msg *telemetrypb.PcapResultV1, encoding string) ([]byte, error) {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "protobuf", "proto", "pb":
		return proto.Marshal(msg)
	default:
		return json.Marshal(msg)
	}
}
