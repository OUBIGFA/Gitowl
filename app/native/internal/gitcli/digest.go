package gitcli

import (
	"crypto/sha256"
	"io"
	"os"
)

func fileDigest(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	h := sha256.New()
	if _, err = io.Copy(h, file); err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}
