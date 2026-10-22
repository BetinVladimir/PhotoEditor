package v2render

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type AuthoredGeometryInputEvidence struct {
	Params CompiledAuthoredGeometryTaskParams
	Inputs v2draft.TaskInputEvidence
}

// AuthoredGeometryTaskPlan is detached compiler output, not a staged/activated
// graph. Its inventory owns copies of the exact actual scoped read evidence.
// The protected writer must repeat current parent/Params/qualification scope.
type AuthoredGeometryTaskPlan struct {
	Run, Workflow uuid.UUID
	Task          v2input.DraftTask
	Params        CompiledAuthoredGeometryTaskParams
	ParamsRef     v2input.Ref
	Entries       []v2input.Entry
	ProfileSHA    string
	OutputBytes   int64
	fingerprint   string
}

func (p AuthoredGeometryTaskPlan) Open(ctx context.Context) (v2input.Cursor, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := p.validateFrozen(); err != nil {
		return nil, err
	}
	return &compositionCursor{entries: append([]v2input.Entry(nil), p.Entries...)}, nil
}

// compileAuthoredGeometryTask deliberately accepts no replacement operation,
// version/backend, source, recipe or invented output reference. The caller's
// profile must be the complete actual profile compiler output, revalidated here.
func compileAuthoredGeometryTask(evidence AuthoredGeometryInputEvidence, paramsRef v2input.Ref, profileRaw []byte) (AuthoredGeometryTaskPlan, error) {
	fail := func() (AuthoredGeometryTaskPlan, error) { return AuthoredGeometryTaskPlan{}, v2input.ErrInvalid }
	params, err := CompileAuthoredGeometryTaskParams(evidence.Params.Params)
	if err != nil || !bytes.Equal(params.JSON, evidence.Params.JSON) || !validLayerArtifactRef(paramsRef) {
		return fail()
	}
	sum := sha256.Sum256(params.JSON)
	if hex.EncodeToString(sum[:]) != paramsRef.SHA256 {
		return fail()
	}
	var profile AuthoredGeometryExecutionProfile
	wire, err := canonical.Bytes(profileRaw)
	if err != nil || !bytes.Equal(wire, profileRaw) || decodeAuthoredParams(profileRaw, &profile) != nil || profile.Backend != "cpu" || profile.Operation != "render.authored_geometry" || profile.Version != "1" || profile.ContractsSHA != AuthoredGeometrySchemasSHA || !profile.RequiredReview || profile.Resources.OutputBytes < 1 {
		return fail()
	}
	// Typed exact field presence must survive zero/default-value decoding.
	encoded, err := canonical.Marshal(profile)
	if err != nil || !bytes.Equal(encoded, profileRaw) {
		return fail()
	}
	if (profile.Renderer != "v2.2" || profile.KernelVersion != "v2.authored.geometry.1") || (profile.TileSize != 256 && profile.TileSize != 512 && profile.TileSize != 1024) || profile.Provider.VRAMBytes != 0 || profile.Resources.VRAMBytes != 0 || profile.Resources.RAMBytes < 1 || profile.Resources.ScratchBytes < 1 {
		return fail()
	}
	if profile.AlphaMode != "require_float32" && profile.AlphaMode != "normalize_uint16" {
		return fail()
	}
	if profile.Qualification.validate() != nil || profile.Qualification.Backend != profile.Backend || profile.Qualification.ModelSHA != profile.ModelSHA || profile.Qualification.RuntimeSHA != profile.RuntimeSHA {
		return fail()
	}
	for _, pin := range []string{profile.RuntimeSHA, profile.ModelSHA, profile.ICCSHA} {
		if _, err := domain.ParseDigest(pin); err != nil {
			return fail()
		}
	}
	run := uuid.NewSHA1(params.Params.InstructionID, []byte("AuthoredGeometryRun/v1"))
	workflow, err := v2input.WorkflowID(run)
	if err != nil {
		return fail()
	}
	task, err := v2input.TaskID(workflow, "render.authored_geometry", 0)
	if err != nil {
		return fail()
	}
	key, err := v2input.NodeKey("render.authored_geometry", 0)
	if err != nil {
		return fail()
	}
	entries := append([]v2input.Entry(nil), evidence.Inputs.Entries...)
	if len(entries) < 1 || len(entries) > 4096 || len(evidence.Inputs.Occurrences) < len(entries) {
		return fail()
	}
	sort.Slice(entries, func(i, j int) bool {
		return bytes.Compare(entries[i].Ref.ArtifactID[:], entries[j].Ref.ArtifactID[:]) < 0
	})
	rootSeen := false
	seen := map[uuid.UUID]bool{}
	for _, entry := range entries {
		if !validLayerArtifactRef(entry.Ref) || seen[entry.Ref.ArtifactID] || entry.Contract == "" || len(entry.Contract) > 120 {
			return fail()
		}
		seen[entry.Ref.ArtifactID] = true
		if entry.Ref.ArtifactID == paramsRef.ArtifactID {
			if entry.Ref != paramsRef || entry.Contract != "Params" {
				return fail()
			}
			rootSeen = true
		}
	}
	if !rootSeen || validateRenderClosureDocuments(evidence.Inputs.Documents, entries) != nil {
		return fail()
	}
	roots, err := AuthoredGeometryTaskInputRoots(params, paramsRef)
	if err != nil {
		return fail()
	}
	for _, root := range roots {
		present := false
		for _, entry := range entries {
			if root == entry {
				present = true
				break
			}
		}
		if !present {
			return fail()
		}
	}
	digest, err := canonical.Hash(struct {
		Task      uuid.UUID       `json:"task"`
		Operation string          `json:"operation"`
		Version   string          `json:"version"`
		Entries   []v2input.Entry `json:"entries"`
	}{task, "render.authored_geometry", "1", entries})
	if err != nil {
		return fail()
	}
	profileSHA := sha256.Sum256(profileRaw)
	// Resource class selects the photographic capacity pool, not the compute
	// device. The distinct singleton segment/1 CPU installation must advertise gpu/cpu; it
	// may not borrow the ordinary CPU-control/precompose registration.
	plan := AuthoredGeometryTaskPlan{Run: run, Workflow: workflow, Task: v2input.DraftTask{ID: task, NodeKey: key, Operation: "render.authored_geometry", OperationVersion: "1", ResourceClass: "gpu", ParamsArtifactID: paramsRef.ArtifactID, ParamsSHA: paramsRef.SHA256, InputDigest: digest, ExecutionProfile: append(json.RawMessage(nil), profileRaw...)}, Params: params, ParamsRef: paramsRef, Entries: entries, ProfileSHA: hex.EncodeToString(profileSHA[:]), OutputBytes: profile.Resources.OutputBytes}
	// Every exported byte/slice remains checked before later publication. This
	// fingerprint is content integrity only, never SQL/qualification authority.
	fingerprint, err := canonical.Hash(plan)
	if err != nil {
		return fail()
	}
	plan.fingerprint = fingerprint
	return plan, nil
}
func (p AuthoredGeometryTaskPlan) validateFrozen() error {
	if p.fingerprint == "" {
		return v2input.ErrInvalid
	}
	actual, err := canonical.Hash(p)
	if err != nil {
		return err
	}
	if actual != p.fingerprint {
		return v2input.ErrInvalid
	}
	return nil
}
