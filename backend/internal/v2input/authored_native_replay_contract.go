package v2input

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

// Exact projection of frozen Python ABI 299646fc. This is content validation,
// not permission to nominate historical artifacts or activate a native task.
type AuthoredNativeProducer struct {
	Run         uuid.UUID `json:"producer_run_id"`
	Task        uuid.UUID `json:"task_id"`
	Result      uuid.UUID `json:"result_id"`
	Operation   string    `json:"operation"`
	Version     string    `json:"operation_version"`
	Attempt     int       `json:"attempt"`
	Fence       int64     `json:"fence"`
	Params      Ref       `json:"params"`
	InputDigest string    `json:"input_digest"`
	ProfileJCS  string    `json:"execution_profile_jcs"`
	ProfileSHA  string    `json:"execution_profile_sha256"`
	Outputs     []Ref     `json:"outputs"`
}
type AuthoredNativeObservation struct {
	Task   AuthoredNativeProducer `json:"task"`
	Root   Ref                    `json:"root"`
	Image  Ref                    `json:"image"`
	Report Ref                    `json:"report"`
}
type AuthoredNativeReplayConsumer struct {
	Tenant         uuid.UUID             `json:"tenant_id"`
	Parent         uuid.UUID             `json:"parent_run_id"`
	Instruction    uuid.UUID             `json:"instruction_id"`
	Ordinal        int                   `json:"ordinal"`
	Operation      string                `json:"operation"`
	Version        string                `json:"operation_version"`
	Task           uuid.UUID             `json:"task_id"`
	Params         Ref                   `json:"params"`
	ProgramSHA     string                `json:"program_sha256"`
	PredecessorSHA string                `json:"predecessor_sha256"`
	Source         SessionVersionBinding `json:"source"`
}
type AuthoredNativeReplayRequest struct {
	SchemaVersion       int                          `json:"schema_version"`
	Consumer            AuthoredNativeReplayConsumer `json:"consumer"`
	GeometryOrdinal     int                          `json:"geometry_ordinal"`
	GeometryInstruction uuid.UUID                    `json:"geometry_instruction_id"`
	GeometryParams      Ref                          `json:"geometry_params"`
	PhaseReceipt        *Ref                         `json:"phase_receipt"`
	Original            AuthoredNativeObservation    `json:"original"`
	NativeSource        SessionVersionBinding        `json:"native_source"`
}

func nativeReplayBinding(b SessionVersionBinding) bool {
	return b.AssetVersionID != uuid.Nil && validRef(b.Source) && digestPattern.MatchString(b.CanvasHash)
}
func nativeReplayProducer(n AuthoredNativeProducer) bool {
	if n.Run == uuid.Nil || n.Task == uuid.Nil || n.Result == uuid.Nil || n.Attempt < 1 || n.Fence < 1 || !validRef(n.Params) || !digestPattern.MatchString(n.InputDigest) || !digestPattern.MatchString(n.ProfileSHA) || len(n.Outputs) < 1 || len(n.Outputs) > 128 {
		return false
	}
	raw, err := canonical.Bytes([]byte(n.ProfileJCS))
	if err != nil || string(raw) != n.ProfileJCS {
		return false
	}
	h := sha256.Sum256(raw)
	if hex.EncodeToString(h[:]) != n.ProfileSHA {
		return false
	}
	var profile map[string]json.RawMessage
	if json.Unmarshal(raw, &profile) != nil || len(profile) == 0 {
		return false
	}
	seen := map[uuid.UUID]bool{}
	for _, ref := range n.Outputs {
		if !validRef(ref) || seen[ref.ArtifactID] {
			return false
		}
		seen[ref.ArtifactID] = true
	}
	return true
}
func DecodeAuthoredNativeReplayRequest(raw []byte) (AuthoredNativeReplayRequest, error) {
	var r AuthoredNativeReplayRequest
	if len(raw) < 2 || len(raw) > 4<<20 {
		return r, ErrInvalid
	}
	exact, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(raw, exact) {
		return r, ErrInvalid
	}
	dec := json.NewDecoder(bytes.NewReader(exact))
	dec.DisallowUnknownFields()
	if dec.Decode(&r) != nil {
		return r, ErrInvalid
	}
	c, n := r.Consumer, r.Original.Task
	if r.SchemaVersion != 1 || c.Tenant == uuid.Nil || c.Parent == uuid.Nil || c.Instruction == uuid.Nil || c.Task == uuid.Nil || c.Ordinal < 1 || c.Ordinal > 4999 || !validRef(c.Params) || !nativeReplayBinding(c.Source) || !digestPattern.MatchString(c.ProgramSHA) || !digestPattern.MatchString(c.PredecessorSHA) || !nativeReplayBinding(r.NativeSource) || r.GeometryOrdinal < 1 || r.GeometryOrdinal > 4998 || r.GeometryInstruction == uuid.Nil || !validRef(r.GeometryParams) || !nativeReplayProducer(n) || n.Run == c.Parent || n.Operation != "image.upscale" || n.Version != "1" || !validRef(r.Original.Root) || !validRef(r.Original.Report) || r.Original.Root != r.Original.Image {
		return r, ErrInvalid
	}
	if !((c.Operation == "render.authored_geometry" && c.Version == "1" && c.Ordinal <= 4998) || (c.Operation == "render.compose" && c.Version == "2")) {
		return r, ErrInvalid
	}
	var profile struct {
		Backend string `json:"backend"`
	}
	if json.Unmarshal([]byte(n.ProfileJCS), &profile) != nil || (profile.Backend != "cuda" && profile.Backend != "mps") {
		return r, ErrInvalid
	}
	root, report := false, false
	for _, ref := range n.Outputs {
		root = root || ref == r.Original.Root
		report = report || ref == r.Original.Report
	}
	if !root || !report {
		return r, ErrInvalid
	}
	if r.GeometryOrdinal == c.Ordinal {
		if c.Operation != "render.authored_geometry" || r.PhaseReceipt != nil || r.GeometryInstruction != c.Instruction || r.GeometryParams != c.Params || r.NativeSource != c.Source {
			return r, ErrInvalid
		}
	} else if r.GeometryOrdinal > c.Ordinal || r.PhaseReceipt == nil || !validRef(*r.PhaseReceipt) {
		return r, ErrInvalid
	}
	normalized, err := canonical.Marshal(r)
	if err != nil || !bytes.Equal(normalized, raw) {
		return r, ErrInvalid
	}
	return r, nil
}
func (r AuthoredNativeReplayRequest) IDs() (uuid.UUID, uuid.UUID, uuid.UUID, Ref, error) {
	raw, err := canonical.Marshal(r)
	if err != nil {
		return uuid.Nil, uuid.Nil, uuid.Nil, Ref{}, err
	}
	if _, err = DecodeAuthoredNativeReplayRequest(raw); err != nil {
		return uuid.Nil, uuid.Nil, uuid.Nil, Ref{}, err
	}
	h := sha256.Sum256(raw)
	sha := hex.EncodeToString(h[:])
	run := uuid.NewSHA1(r.GeometryInstruction, []byte("AuthoredNativeReplay/v1:"+sha))
	workflow := uuid.NewSHA1(run, []byte("workflow"))
	task := uuid.NewSHA1(workflow, []byte("image.upscale:0"))
	return run, workflow, task, Ref{ArtifactID: uuid.NewSHA1(run, []byte("request")), SHA256: sha}, nil
}
