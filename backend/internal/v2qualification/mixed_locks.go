package v2qualification

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"photoeditor/backend/internal/canonical"
)

// Operator-provisioned parent locks do not substitute for any child report.
type MixedDeploymentLocks struct {
	Schema     int               `json:"schema_version"`
	Scope      string            `json:"scope"`
	Common     DeploymentLocks   `json:"common"`
	Children   []MixedChild      `json:"children"`
	Routes     map[string]string `json:"stage_routes"`
	MaxP95     float64           `json:"max_p95_ms"`
	MaxRSS     float64           `json:"max_rss_bytes"`
	MaxScratch float64           `json:"max_scratch_bytes"`
}

func (l MixedDeploymentLocks) Expected() (MixedExpected, error) {
	var empty MixedExpected
	common, err := l.Common.Expected()
	if err != nil || l.Schema != 2 || l.Scope != "renderer.staged.mixed" || common.Operation != "renderer.staged" || common.Version != "1" || common.Backend != "cpu" || common.MPSResources != nil {
		return empty, ErrUnqualified
	}
	for _, v := range []float64{l.MaxP95, l.MaxRSS, l.MaxScratch} {
		if !bounded(v) || v <= 0 {
			return empty, ErrUnqualified
		}
	}
	raw, err := canonical.Marshal(struct {
		Children []MixedChild      `json:"children"`
		Routes   map[string]string `json:"routes"`
	}{l.Children, l.Routes})
	if err != nil {
		return empty, err
	}
	var owned struct {
		Children []MixedChild      `json:"children"`
		Routes   map[string]string `json:"routes"`
	}
	if json.Unmarshal(raw, &owned) != nil {
		return empty, ErrUnqualified
	}
	children, err := canonical.Marshal(owned.Children)
	if err != nil {
		return empty, err
	}
	sum := sha256.Sum256(children)
	a := MixedAuthority{Children: owned.Children, Routes: owned.Routes, DependenciesSHA: hex.EncodeToString(sum[:])}
	if a.validateDependencies() != nil {
		return empty, ErrUnqualified
	}
	required := map[string]bool{}
	for _, id := range common.RequiredChecks {
		required[id] = true
	}
	for _, id := range []string{"rgb_golden", "alpha_golden", "generated_golden", "finite", "protected_unchanged", "outside_roi_unchanged", "stage_dependency_binding"} {
		if !required[id] {
			return empty, ErrUnqualified
		}
	}
	return MixedExpected{Common: common, Children: owned.Children, Routes: owned.Routes, MaxP95: l.MaxP95, MaxRSS: l.MaxRSS, MaxScratch: l.MaxScratch}, nil
}
func LoadMixedLocks(path string) (MixedExpected, error) {
	var empty MixedExpected
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() || before.Mode().Perm()&022 != 0 || before.Size() < 2 || before.Size() > 1024*1024 {
		return empty, ErrUnqualified
	}
	f, err := os.Open(path)
	if err != nil {
		return empty, ErrUnqualified
	}
	defer f.Close()
	observed, err := f.Stat()
	if err != nil || !os.SameFile(before, observed) {
		return empty, ErrUnqualified
	}
	raw, err := io.ReadAll(io.LimitReader(f, 1024*1024+1))
	if err != nil || int64(len(raw)) != before.Size() {
		return empty, ErrUnqualified
	}
	after, err := f.Stat()
	if err != nil || !os.SameFile(before, after) || after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
		return empty, ErrUnqualified
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	var locks MixedDeploymentLocks
	if d.Decode(&locks) != nil || d.Decode(new(any)) != io.EOF {
		return empty, ErrUnqualified
	}
	return locks.Expected()
}
