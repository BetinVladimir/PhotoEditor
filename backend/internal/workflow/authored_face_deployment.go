package workflow

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"io"
	"math"
	"os"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
	"photoeditor/backend/internal/v2render"
)

var ErrAuthoredFaceUnavailable = errors.New("AUTHORED_FACE_DEPLOYMENT_UNAVAILABLE")

// Deployment is operator configuration, not an API request or a qualification
// issuer. Every operation still verifies the physical report and current SQL
// policy at each protected admission. No weights/reports are auto-discovered.
type AuthoredFaceOperationDeployment struct {
	Expected v2qualification.Expected `json:"expected"`
	Report   v2input.Ref              `json:"report"`
	RAM      int64                    `json:"ram_bytes"`
	VRAM     int64                    `json:"vram_bytes"`
	Scratch  int64                    `json:"scratch_bytes"`
	Output   int64                    `json:"output_bytes"`
}
type AuthoredFaceDeployment struct {
	SchemaVersion int                             `json:"schema_version"`
	Tenants       []uuid.UUID                     `json:"tenants"`
	Detector      AuthoredFaceOperationDeployment `json:"detector"`
	Landmarks     AuthoredFaceOperationDeployment `json:"landmarks"`
	Parser        AuthoredFaceOperationDeployment `json:"parser"`
	Skin          AuthoredFaceOperationDeployment `json:"skin"`
	Score         float64                         `json:"score_threshold"`
	MaxFaces      int                             `json:"max_faces"`
	PreviewPolicy string                          `json:"preview_policy"`
}

func (d AuthoredFaceOperationDeployment) budget() v2render.AuthoredFaceBudget {
	return v2render.AuthoredFaceBudget{Backend: d.Expected.Backend, RAM: d.RAM, VRAM: d.VRAM, Scratch: d.Scratch, Output: d.Output}
}
func deploymentDigest(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func (d AuthoredFaceDeployment) validate() error {
	if d.SchemaVersion != 1 || len(d.Tenants) == 0 || len(d.Tenants) > 256 || math.IsNaN(d.Score) || math.IsInf(d.Score, 0) || d.Score < 0 || d.Score > 1 || d.MaxFaces < 1 || d.MaxFaces > 64 || (d.PreviewPolicy != "legacy_v1" && d.PreviewPolicy != "declared_encoding_alpha8_icc_v2") {
		return ErrAuthoredFaceUnavailable
	}
	seen := map[uuid.UUID]bool{}
	for _, id := range d.Tenants {
		if id == uuid.Nil || seen[id] {
			return ErrAuthoredFaceUnavailable
		}
		seen[id] = true
	}
	for i, p := range []AuthoredFaceOperationDeployment{d.Detector, d.Landmarks, d.Parser, d.Skin} {
		e := p.Expected
		op := []string{"face.detect", "face.landmarks", "face.parse", "mask.face_skin"}[i]
		if e.Operation != op || e.Version != "1" || !e.LicenseCleared || e.IssuerID == uuid.Nil || len(e.RequiredChecks) == 0 || len(e.CalibrationSHAs) == 0 || len(e.TestFixtureSHAs) == 0 || len(e.EvidenceSHAs) == 0 || !deploymentDigest(e.ModelLockSHA) || !deploymentDigest(e.RuntimeLockSHA) || !deploymentDigest(e.DeviceProfileSHA) || !deploymentDigest(e.DatasetSHA) || p.Report.ArtifactID == uuid.Nil || !deploymentDigest(p.Report.SHA256) || p.RAM <= 0 || p.Scratch <= 0 || p.Output <= 0 || p.RAM > 9007199254740991 || p.Scratch > 9007199254740991 || p.Output > 9007199254740991 || p.VRAM > 9007199254740991 {
			return ErrAuthoredFaceUnavailable
		}
		if i < 3 && (e.Backend != d.Detector.Expected.Backend || e.RuntimeLockSHA != d.Detector.Expected.RuntimeLockSHA || e.DeviceProfileSHA != d.Detector.Expected.DeviceProfileSHA) {
			return ErrAuthoredFaceUnavailable
		}
		if i == 3 {
			if e.Backend != "cpu" || p.VRAM != 0 {
				return ErrAuthoredFaceUnavailable
			}
		} else if e.Backend != "cuda" && e.Backend != "mps" || p.VRAM <= 0 {
			return ErrAuthoredFaceUnavailable
		}
		checks := map[string]bool{}
		for _, c := range e.RequiredChecks {
			if c == "" || checks[c] {
				return ErrAuthoredFaceUnavailable
			}
			checks[c] = true
		}
		for _, set := range []map[string]struct{}{e.CalibrationSHAs, e.TestFixtureSHAs, e.EvidenceSHAs} {
			for sha := range set {
				if !deploymentDigest(sha) {
					return ErrAuthoredFaceUnavailable
				}
			}
		}
	}
	return nil
}
func LoadAuthoredFaceDeployment(path, wantSHA string) (AuthoredFaceDeployment, error) {
	var zero AuthoredFaceDeployment
	if path == "" || !deploymentDigest(wantSHA) {
		return zero, ErrAuthoredFaceUnavailable
	}
	st, err := os.Lstat(path)
	if err != nil || !st.Mode().IsRegular() || st.Size() < 2 || st.Size() > 1048576 {
		return zero, ErrAuthoredFaceUnavailable
	}
	f, err := os.Open(path)
	if err != nil {
		return zero, ErrAuthoredFaceUnavailable
	}
	defer f.Close()
	actual, err := f.Stat()
	if err != nil || !os.SameFile(st, actual) {
		return zero, ErrAuthoredFaceUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(f, 1048577))
	if err != nil || len(raw) > 1048576 {
		return zero, ErrAuthoredFaceUnavailable
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != wantSHA {
		return zero, ErrAuthoredFaceUnavailable
	}
	closed, err := canonical.Marshal(json.RawMessage(raw))
	if err != nil || !bytes.Equal(closed, raw) {
		return zero, ErrAuthoredFaceUnavailable
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var out AuthoredFaceDeployment
	if dec.Decode(&out) != nil || dec.Decode(new(any)) != io.EOF {
		return zero, ErrAuthoredFaceUnavailable
	}
	if err = out.validate(); err != nil {
		return zero, err
	}
	return out, nil
}
