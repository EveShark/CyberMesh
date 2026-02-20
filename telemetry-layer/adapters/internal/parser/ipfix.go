package parser

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"cybermesh/telemetry-layer/adapters/internal/model"
	"github.com/calmh/ipfix"
)

func RecordFromIPFIX(fields []ipfix.InterpretedField) (model.Record, error) {
	var rec model.Record
	var startMS uint64
	var endMS uint64
	for _, f := range fields {
		name := strings.ToLower(strings.TrimSpace(f.Name))
		switch name {
		case "sourceipv4address", "sourceipv6address":
			rec.SrcIP = ipValue(f.Value)
		case "destinationipv4address", "destinationipv6address":
			rec.DstIP = ipValue(f.Value)
		case "sourcetransportport":
			rec.SrcPort = int(numberValue(f.Value))
		case "destinationtransportport":
			rec.DstPort = int(numberValue(f.Value))
		case "protocolidentifier":
			rec.Proto = int(numberValue(f.Value))
		case "packetdeltacount":
			rec.PktsFwd = int64(numberValue(f.Value))
		case "octetdeltacount":
			rec.BytesFwd = int64(numberValue(f.Value))
		case "flowstartmilliseconds":
			startMS = numberValue(f.Value)
			rec.Timestamp = int64(startMS / 1000)
		case "flowendmilliseconds":
			endMS = numberValue(f.Value)
			if startMS > 0 && endMS >= startMS {
				rec.DurationMS = int64(endMS - startMS)
				rec.TimingKnown = true
			}
		case "tcpcontrolbits":
			flags := uint16(numberValue(f.Value))
			if flags != 0 {
				rec.FlagsKnown = true
				if flags&0x01 != 0 {
					rec.FinFlagCnt = 1
				}
				if flags&0x02 != 0 {
					rec.SynFlagCnt = 1
				}
				if flags&0x04 != 0 {
					rec.RstFlagCnt = 1
				}
				if flags&0x08 != 0 {
					rec.PshFlagCnt = 1
				}
				if flags&0x10 != 0 {
					rec.AckFlagCnt = 1
				}
				if flags&0x20 != 0 {
					rec.UrgFlagCnt = 1
				}
				if flags&0x40 != 0 {
					rec.EceFlagCnt = 1
				}
				if flags&0x80 != 0 {
					rec.CweFlagCount = 1
				}
			}
		}
	}
	if rec.PktsFwd > 0 || rec.BytesFwd > 0 {
		rec.MetricsKnown = true
	}
	if rec.SrcIP == "" || rec.DstIP == "" {
		return model.Record{}, errors.New("missing src/dst ip")
	}
	return rec, nil
}

func ipValue(value any) string {
	switch v := value.(type) {
	case net.IP:
		return v.String()
	case string:
		return v
	default:
		return fmt.Sprintf("%v", v)
	}
}

func numberValue(value any) uint64 {
	switch v := value.(type) {
	case uint8:
		return uint64(v)
	case uint16:
		return uint64(v)
	case uint32:
		return uint64(v)
	case uint64:
		return v
	case int:
		return uint64(v)
	case int64:
		return uint64(v)
	case float64:
		return uint64(v)
	case time.Time:
		return uint64(v.UnixMilli())
	default:
		return 0
	}
}
