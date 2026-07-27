package modules

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
)

// FileDigest reports the hex-encoded SHA-256 digest of a file's contents.
func FileDigest(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// BytesDigest reports the hex-encoded SHA-256 digest of the given bytes.
func BytesDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
