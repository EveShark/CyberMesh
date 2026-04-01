package api

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"time"
)

const controlRuntimeStateKeyMutationsSafeMode = "control_mutations_safe_mode"
const controlRuntimeStateKeyMutationsKillSwitch = "control_mutations_kill_switch"

type controlSafeModeState struct {
	Enabled    bool
	ReasonCode string
	ReasonText string
	UpdatedAt  time.Time
}

type controlRuntimeToggleMutation struct {
	StateKey       string
	ActionType     string
	Enabled        bool
	Actor          string
	ReasonCode     string
	ReasonText     string
	IdempotencyKey string
	RequestID      string
	TenantScope    string
}

func loadControlSafeModeState(ctx context.Context, db *sql.DB) (controlSafeModeState, bool, error) {
	var state controlSafeModeState
	err := db.QueryRowContext(ctx, `
		SELECT enabled, reason_code, reason_text, updated_at
		FROM control_runtime_state
		WHERE state_key = $1
	`, controlRuntimeStateKeyMutationsSafeMode).Scan(
		&state.Enabled,
		&state.ReasonCode,
		&state.ReasonText,
		&state.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return controlSafeModeState{}, false, nil
	}
	if err != nil {
		return controlSafeModeState{}, false, err
	}
	return state, true, nil
}

func storeControlSafeModeState(ctx context.Context, db *sql.DB, enabled bool, reasonCode, reasonText string) error {
	_, err := db.ExecContext(ctx, `
		UPSERT INTO control_runtime_state (state_key, enabled, reason_code, reason_text, updated_at)
		VALUES ($1, $2, $3, $4, now())
	`, controlRuntimeStateKeyMutationsSafeMode, enabled, reasonCode, reasonText)
	return err
}

func storeControlSafeModeStateTx(ctx context.Context, tx *sql.Tx, enabled bool, reasonCode, reasonText string) error {
	_, err := tx.ExecContext(ctx, `
		UPSERT INTO control_runtime_state (state_key, enabled, reason_code, reason_text, updated_at)
		VALUES ($1, $2, $3, $4, now())
	`, controlRuntimeStateKeyMutationsSafeMode, enabled, reasonCode, reasonText)
	return err
}

func (s *Server) currentControlMutationSafeModeState(ctx context.Context) (bool, error) {
	if s.storage == nil {
		return s.controlMutationsSafeMode.Load(), nil
	}
	db, err := s.getDB()
	if err != nil {
		return s.controlMutationsSafeMode.Load(), err
	}
	state, ok, err := loadControlSafeModeState(ctx, db)
	if err != nil {
		return s.controlMutationsSafeMode.Load(), err
	}
	if ok {
		s.controlMutationsSafeMode.Store(state.Enabled)
		if s.controlSafeMode != nil {
			s.controlSafeMode.Store(state.Enabled)
		}
		return state.Enabled, nil
	}
	return s.controlMutationsSafeMode.Load(), nil
}

func (s *Server) currentControlMutationSafeMode(ctx context.Context) bool {
	enabled, err := s.currentControlMutationSafeModeState(ctx)
	if err != nil {
		return s.controlMutationsSafeMode.Load()
	}
	return enabled
}

func (s *Server) currentControlMutationSafeModeWithTimeout() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return s.currentControlMutationSafeMode(ctx)
}

type controlKillSwitchState struct {
	Enabled    bool
	ReasonCode string
	ReasonText string
	UpdatedAt  time.Time
}

func loadControlKillSwitchState(ctx context.Context, db *sql.DB) (controlKillSwitchState, bool, error) {
	var state controlKillSwitchState
	err := db.QueryRowContext(ctx, `
		SELECT enabled, reason_code, reason_text, updated_at
		FROM control_runtime_state
		WHERE state_key = $1
	`, controlRuntimeStateKeyMutationsKillSwitch).Scan(
		&state.Enabled,
		&state.ReasonCode,
		&state.ReasonText,
		&state.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return controlKillSwitchState{}, false, nil
	}
	if err != nil {
		return controlKillSwitchState{}, false, err
	}
	return state, true, nil
}

func storeControlKillSwitchState(ctx context.Context, db *sql.DB, enabled bool, reasonCode, reasonText string) error {
	_, err := db.ExecContext(ctx, `
		UPSERT INTO control_runtime_state (state_key, enabled, reason_code, reason_text, updated_at)
		VALUES ($1, $2, $3, $4, now())
	`, controlRuntimeStateKeyMutationsKillSwitch, enabled, reasonCode, reasonText)
	return err
}

func storeControlKillSwitchStateTx(ctx context.Context, tx *sql.Tx, enabled bool, reasonCode, reasonText string) error {
	_, err := tx.ExecContext(ctx, `
		UPSERT INTO control_runtime_state (state_key, enabled, reason_code, reason_text, updated_at)
		VALUES ($1, $2, $3, $4, now())
	`, controlRuntimeStateKeyMutationsKillSwitch, enabled, reasonCode, reasonText)
	return err
}

func loadControlRuntimeToggleStateTx(ctx context.Context, tx *sql.Tx, stateKey string) (bool, bool, error) {
	var enabled bool
	err := tx.QueryRowContext(ctx, `
		SELECT enabled
		FROM control_runtime_state
		WHERE state_key = $1
	`, stateKey).Scan(&enabled)
	if err == sql.ErrNoRows {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	return enabled, true, nil
}

func persistControlRuntimeToggle(ctx context.Context, db *sql.DB, mutation controlRuntimeToggleMutation) (string, error) {
	if db == nil {
		return "", fmt.Errorf("db required")
	}
	if mutation.StateKey == "" {
		return "", fmt.Errorf("state key required")
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	beforeEnabled, beforeExists, err := loadControlRuntimeToggleStateTx(ctx, tx, mutation.StateKey)
	if err != nil {
		return "", err
	}

	switch mutation.StateKey {
	case controlRuntimeStateKeyMutationsSafeMode:
		err = storeControlSafeModeStateTx(ctx, tx, mutation.Enabled, mutation.ReasonCode, mutation.ReasonText)
	case controlRuntimeStateKeyMutationsKillSwitch:
		err = storeControlKillSwitchStateTx(ctx, tx, mutation.Enabled, mutation.ReasonCode, mutation.ReasonText)
	default:
		err = fmt.Errorf("unsupported control runtime state key %q", mutation.StateKey)
	}
	if err != nil {
		return "", err
	}

	beforeStatus := "unset"
	if beforeExists {
		beforeStatus = strconv.FormatBool(beforeEnabled)
	}
	actionID, err := insertControlActionJournal(ctx, tx, controlActionJournalInsert{
		ActionType:     mutation.ActionType,
		TargetKind:     "lease",
		LeaseKey:       mutation.StateKey,
		Actor:          mutation.Actor,
		ReasonCode:     mutation.ReasonCode,
		ReasonText:     mutation.ReasonText,
		IdempotencyKey: mutation.IdempotencyKey,
		RequestID:      mutation.RequestID,
		BeforeStatus:   beforeStatus,
		AfterStatus:    strconv.FormatBool(mutation.Enabled),
		TenantScope:    mutation.TenantScope,
	})
	if err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return actionID, nil
}

func (s *Server) currentControlMutationKillSwitchState(ctx context.Context) (bool, error) {
	if s.storage == nil {
		return s.controlMutationsKillSwitch.Load(), nil
	}
	db, err := s.getDB()
	if err != nil {
		return s.controlMutationsKillSwitch.Load(), err
	}
	state, ok, err := loadControlKillSwitchState(ctx, db)
	if err != nil {
		return s.controlMutationsKillSwitch.Load(), err
	}
	if ok {
		s.controlMutationsKillSwitch.Store(state.Enabled)
		return state.Enabled, nil
	}
	return s.controlMutationsKillSwitch.Load(), nil
}

func (s *Server) currentControlMutationKillSwitch(ctx context.Context) bool {
	enabled, err := s.currentControlMutationKillSwitchState(ctx)
	if err != nil {
		return s.controlMutationsKillSwitch.Load()
	}
	return enabled
}

func (s *Server) currentControlMutationKillSwitchWithTimeout() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return s.currentControlMutationKillSwitch(ctx)
}
