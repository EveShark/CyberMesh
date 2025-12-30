package cockroach

import (
	"context"
	"errors"
	"net"

	"github.com/jackc/pgconn"
	"github.com/lib/pq"
)

// IsRetryable reports whether an error is safe to retry for CockroachDB persistence operations.
// It is intentionally conservative: integrity/data errors are non-retryable, while transient
// transport/serialization failures are retryable.
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrIntegrityViolation) || errors.Is(err, ErrInvalidData) {
		return false
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "40001": // serialization_failure
			return true
		case "40P01": // deadlock_detected
			return true
		case "55P03": // lock_not_available
			return true
		case "53300": // too_many_connections
			return true
		case "57P01", "57P02", "57P03": // admin_shutdown, crash_shutdown, cannot_connect_now
			return true
		default:
			return true
		}
	}

	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		code := string(pqErr.Code)
		switch code {
		case "40001", "40P01", "55P03", "53300", "57P01", "57P02", "57P03":
			return true
		default:
			return true
		}
	}

	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout() || netErr.Temporary()
	}

	// Default to retryable; PersistenceWorker has bounded retries.
	return true
}
