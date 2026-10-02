// Package colorprofile validates the deployed ICC registry without invoking a
// color engine. It supplies trusted profile hashes for backend artifact checks.
package colorprofile

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"photoeditor/backend/internal/canonical"
)

type entry struct {
	ID        string `json:"id"`
	Path      string `json:"path"`
	SHA256    string `json:"sha256"`
	Bytes     int64  `json:"bytes"`
	License   string `json:"license"`
	Generator int    `json:"generator_lcms_version"`
}
type manifest struct {
	Version  int     `json:"format_version"`
	Profiles []entry `json:"profiles"`
}

// LoadWorkSHA verifies all three bundled profiles against the local lock file.
// The directory and lock file are deployment configuration, never user input.
func LoadWorkSHA(directory string) (string, error) {
	invalid := errors.New("invalid ICC registry")
	if directory == "" {
		return "", invalid
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return "", invalid
	}
	defer root.Close()
	read := func(name string, limit int64) ([]byte, error) {
		info, e := root.Lstat(name)
		if e != nil || !info.Mode().IsRegular() || info.Size() > limit {
			return nil, invalid
		}
		file, e := root.Open(name)
		if e != nil {
			return nil, invalid
		}
		defer file.Close()
		raw, e := io.ReadAll(io.LimitReader(file, limit+1))
		if e != nil || int64(len(raw)) > limit {
			return nil, invalid
		}
		return raw, nil
	}
	raw, err := read("profiles.lock.json", 65536)
	if err != nil {
		return "", err
	}
	normalized, err := canonical.Bytes(raw)
	if err != nil {
		return "", invalid
	}
	var lock manifest
	dec := json.NewDecoder(bytes.NewReader(normalized))
	dec.DisallowUnknownFields()
	if dec.Decode(&lock) != nil {
		return "", invalid
	}
	roundtrip, err := canonical.Marshal(lock)
	if err != nil || !bytes.Equal(roundtrip, normalized) || lock.Version != 1 || len(lock.Profiles) != 3 {
		return "", invalid
	}
	allowed := map[string]bool{"work_rec2020_gamma24_v1": true, "srgb_v1": true, "linear_srgb_v1": true}
	result := ""
	for _, p := range lock.Profiles {
		if !allowed[p.ID] || p.Path != p.ID+".icc" || p.Bytes < 132 || p.Bytes > 1048576 || p.License == "" || p.Generator <= 0 {
			return "", invalid
		}
		delete(allowed, p.ID)
		data, e := read(p.Path, 1048576)
		if e != nil || int64(len(data)) != p.Bytes {
			return "", invalid
		}
		hash := sha256.Sum256(data)
		if hex.EncodeToString(hash[:]) != p.SHA256 || !validICC(data) {
			return "", invalid
		}
		if p.ID == "work_rec2020_gamma24_v1" {
			result = p.SHA256
		}
	}
	if result == "" || len(allowed) != 0 {
		return "", invalid
	}
	return result, nil
}

// LoadSHAs returns the complete deployment-verified profile map. Validation is
// delegated to LoadWorkSHA so callers cannot consume hashes from an unchecked
// lock file.
func LoadSHAs(directory string) (map[string]string, error) {
	if _, err := LoadWorkSHA(directory); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, errors.New("invalid ICC registry")
	}
	defer root.Close()
	file, err := root.Open("profiles.lock.json")
	if err != nil {
		return nil, errors.New("invalid ICC registry")
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, 65537))
	if err != nil || len(raw) > 65536 {
		return nil, errors.New("invalid ICC registry")
	}
	var lock manifest
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&lock) != nil {
		return nil, errors.New("invalid ICC registry")
	}
	result := make(map[string]string, len(lock.Profiles))
	for _, profile := range lock.Profiles {
		result[profile.ID] = profile.SHA256
	}
	if len(result) != 3 {
		return nil, errors.New("invalid ICC registry")
	}
	return result, nil
}
func validICC(data []byte) bool {
	if len(data) < 132 || len(data) > 1048576 || int(binary.BigEndian.Uint32(data[:4])) != len(data) || string(data[36:40]) != "acsp" || string(data[16:20]) != "RGB " || (data[8] != 2 && data[8] != 4) {
		return false
	}
	count := uint64(binary.BigEndian.Uint32(data[128:132]))
	end := uint64(132) + count*12
	if count > 4096 || end > uint64(len(data)) {
		return false
	}
	for i := uint64(0); i < count; i++ {
		start := 132 + i*12
		offset := uint64(binary.BigEndian.Uint32(data[start+4 : start+8]))
		size := uint64(binary.BigEndian.Uint32(data[start+8 : start+12]))
		if offset < end || size < 8 || offset+size > uint64(len(data)) {
			return false
		}
	}
	return true
}
