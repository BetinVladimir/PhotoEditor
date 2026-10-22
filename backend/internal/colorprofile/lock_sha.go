package colorprofile

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
)

// LoadLockSHA returns the raw-byte pin used by worker presentation documents.
// Validate the registry before and after the bounded read to detect replacement.
func LoadLockSHA(directory string) (string, error) {
	if _, err := LoadSHAs(directory); err != nil {
		return "", err
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return "", err
	}
	defer root.Close()
	read := func() ([]byte, error) {
		info, err := root.Lstat("profiles.lock.json")
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || info.Size() < 2 || info.Size() > 65536 {
			return nil, errors.New("invalid ICC lock")
		}
		f, err := root.Open("profiles.lock.json")
		if err != nil {
			return nil, err
		}
		defer f.Close()
		raw, err := io.ReadAll(io.LimitReader(f, 65537))
		if err != nil {
			return nil, err
		}
		if int64(len(raw)) != info.Size() {
			return nil, errors.New("ICC lock changed")
		}
		return raw, nil
	}
	raw, err := read()
	if err != nil {
		return "", err
	}
	if _, err := LoadSHAs(directory); err != nil {
		return "", err
	}
	again, err := read()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	if sha256.Sum256(again) != sum {
		return "", errors.New("ICC lock changed")
	}
	return hex.EncodeToString(sum[:]), nil
}
