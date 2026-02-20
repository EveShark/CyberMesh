package validate

import (
	"errors"

	"cybermesh/telemetry-layer/adapters/internal/model"
)

func DeepFlow(event model.DeepFlowEvent) error {
	if event.Schema == "" {
		return errors.New("schema required")
	}
	if event.Timestamp == 0 {
		return errors.New("timestamp required")
	}
	if event.TenantID == "" {
		return errors.New("tenant_id required")
	}
	if event.SensorID == "" {
		return errors.New("sensor_id required")
	}
	if event.AlertType == "" {
		return errors.New("alert_type required")
	}
	return nil
}
