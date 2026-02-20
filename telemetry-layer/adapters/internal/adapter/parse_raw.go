package adapter

import (
	"cybermesh/telemetry-layer/adapters/internal/parser"
)

func parseGCP(line string) ([]Record, error) {
	return parser.ParseGCPVPC([]byte(line))
}

func parseAWS(line string) ([]Record, error) {
	return parser.ParseAWSVPCText(line)
}

func parseAzure(line string) ([]Record, error) {
	return parser.ParseAzureNSGFlowTuples([]byte(line))
}
