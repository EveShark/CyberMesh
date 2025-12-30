package leader

import "encoding/binary"

// Canonical encoding helper functions

func appendUint64(buf []byte, val uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, val)
	return append(buf, b...)
}

func appendInt64(buf []byte, val int64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(val))
	return append(buf, b...)
}
