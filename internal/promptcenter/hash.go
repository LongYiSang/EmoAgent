package promptcenter

import (
	"crypto/sha256"
	"encoding/hex"
)

func HashText(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}
