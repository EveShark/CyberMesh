package mempool

import "errors"

// ClassFullReason returns the saturated priority class for ErrPriorityClassFull.
func ClassFullReason(err error) (string, bool) {
	var classErr ErrClassFull
	if !errors.As(err, &classErr) {
		return "", false
	}
	return normalizePriorityClass(classErr.Class), true
}
