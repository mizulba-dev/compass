package api

import (
	"crypto/rand"
	"encoding/base32"
	"strings"
)

// newLocalID returns a short random id for entities nested inside the
// canvas JSON blob (gaps, tasks, policies). It does not need the collision
// resistance of a canvas id, only uniqueness within one canvas.
func newLocalID() string {
	buf := make([]byte, 6)
	_, _ = rand.Read(buf)
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf))
}
