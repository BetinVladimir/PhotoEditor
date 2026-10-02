package v2input

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"photoeditor/backend/internal/canonical"
	"regexp"
	"syscall"
	"unicode/utf8"
)

type LensRegistryConfiguration struct {
	RuntimeLockSHA string
	Pins           []LensQualificationPin
}

// LoadLensRegistry reads operator configuration, not user-supplied recipe data.
// The expected raw SHA must be supplied independently by deployment settings.
// Backend pins the runtime document; worker verifies installed runtime binaries.
func LoadLensRegistry(path, expectedSHA string) (LensRegistryConfiguration, error) {
	var empty LensRegistryConfiguration
	if path == "" || !digestPattern.MatchString(expectedSHA) {
		return empty, ErrInvalid
	}
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return empty, err
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		syscall.Close(fd)
		return empty, ErrInvalid
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return empty, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || stat.Nlink != 1 || info.Size() < 1 || info.Size() > 1048576 {
		file.Close()
		return empty, ErrInvalid
	}
	raw, readErr := io.ReadAll(io.LimitReader(file, 1048577))
	closeErr := file.Close()
	if readErr != nil {
		return empty, readErr
	}
	if closeErr != nil {
		return empty, closeErr
	}
	sum := sha256.Sum256(raw)
	if int64(len(raw)) != info.Size() || hex.EncodeToString(sum[:]) != expectedSHA {
		return empty, ErrInvalid
	}
	if _, err = canonical.Bytes(raw); err != nil {
		return empty, ErrInvalid
	}
	var registry struct {
		SchemaVersion int             `json:"schema_version"`
		Runtime       json.RawMessage `json:"runtime"`
		Profiles      []struct {
			Qualification   Ref    `json:"qualification"`
			CoefficientsSHA string `json:"coefficients_sha"`
			RuntimeLockSHA  string `json:"runtime_lock_sha"`
			DatasetSHA      string `json:"dataset_sha"`
		} `json:"profiles"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&registry) != nil || decoder.Decode(new(any)) != io.EOF || registry.SchemaVersion != 1 || registry.Profiles == nil || len(registry.Profiles) > 10000 {
		return empty, ErrInvalid
	}
	var runtime map[string]json.RawMessage
	if json.Unmarshal(registry.Runtime, &runtime) != nil || !validLensRuntimeDocument(registry.Runtime) {
		return empty, ErrInvalid
	}
	runtimeSHA, err := canonical.Hash(runtime)
	if err != nil {
		return empty, err
	}
	result := LensRegistryConfiguration{RuntimeLockSHA: runtimeSHA, Pins: make([]LensQualificationPin, 0, len(registry.Profiles))}
	seen := map[string]bool{}
	for _, profile := range registry.Profiles {
		if !validRef(profile.Qualification) || !digestPattern.MatchString(profile.CoefficientsSHA) || !digestPattern.MatchString(profile.DatasetSHA) || profile.RuntimeLockSHA != runtimeSHA {
			return empty, ErrInvalid
		}
		key := profile.Qualification.ArtifactID.String() + ":" + profile.Qualification.SHA256 + ":" + profile.CoefficientsSHA
		if seen[key] {
			return empty, ErrInvalid
		}
		seen[key] = true
		result.Pins = append(result.Pins, LensQualificationPin{profile.Qualification, profile.CoefficientsSHA, profile.RuntimeLockSHA, profile.DatasetSHA})
	}
	return result, nil
}

func validLensRuntimeDocument(raw []byte) bool {
	if !exactJSONKeys(raw, "schema_version", "python_version", "implementation", "machine", "packages", "components") {
		return false
	}
	var lock struct {
		SchemaVersion  int               `json:"schema_version"`
		PythonVersion  string            `json:"python_version"`
		Implementation string            `json:"implementation"`
		Machine        string            `json:"machine"`
		Packages       map[string]string `json:"packages"`
		Components     map[string]string `json:"components"`
	}
	if json.Unmarshal(raw, &lock) != nil || lock.SchemaVersion != 1 || lock.Implementation != "CPython" || !regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`).MatchString(lock.PythonVersion) || utf8.RuneCountInString(lock.Machine) < 1 || utf8.RuneCountInString(lock.Machine) > 80 || len(lock.Packages) != 3 || len(lock.Components) != 13 {
		return false
	}
	for _, name := range []string{"numpy", "pydantic", "rfc8785"} {
		value, ok := lock.Packages[name]
		if !ok || utf8.RuneCountInString(value) < 1 || utf8.RuneCountInString(value) > 120 {
			return false
		}
	}
	for _, name := range []string{"python", "numpy_core", "pydantic_core", "canonical_impl", "lens_algorithm", "lens_prepare", "lens_color", "lens_adapter", "lens_result", "lens_resources", "lens_admission", "lens_metadata_source", "lens_exif"} {
		if !digestPattern.MatchString(lock.Components[name]) {
			return false
		}
	}
	return true
}
