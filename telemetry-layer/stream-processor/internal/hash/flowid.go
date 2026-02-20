package hash

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

func FlowID(key string, windowStart int64) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%d", key, windowStart)))
	return hex.EncodeToString(sum[:])
}
