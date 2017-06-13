package core

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"strings"
	"sync/atomic"
	"time"
)

// unique id salt and ctr
var (
	salt = "🌨"
	ctr  uint64
)

// generateToken will generate a token that is about as unique as you can hope for
func generateToken(prefix ...string) string {
	hasher := sha256.New()

	hasher.Write([]byte(salt))                                             //nolint (errcheck)
	binary.Write(hasher, binary.LittleEndian, time.Now().UTC().UnixNano()) //nolint (errcheck)
	binary.Write(hasher, binary.LittleEndian, atomic.AddUint64(&ctr, 1))   //nolint (errcheck)

	return strings.Join(prefix, "-") + base64.URLEncoding.EncodeToString(hasher.Sum(nil))[:16]
}
