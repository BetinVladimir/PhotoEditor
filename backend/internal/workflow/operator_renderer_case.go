package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"io"
	"path/filepath"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2qualification"
	"reflect"
	"sort"
)

// OperatorCaseSpec is loaded only from the externally approved pinned corpus.
// Objects are physical prepared inputs, not READY/canonical/accepted SQL rows.
type OperatorCaseSpec struct {
	Schema          int                              `json:"schema_version"`
	ID              string                           `json:"case_id"`
	Tenant          uuid.UUID                        `json:"tenant_id"`
	Run             uuid.UUID                        `json:"run_id"`
	Actor           uuid.UUID                        `json:"actor_id"`
	CorpusSHA       string                           `json:"corpus_sha256"`
	Sources         []applyInput                     `json:"sources"`
	Objects         []styleArtifact                  `json:"objects"`
	Contracts       map[uuid.UUID]string             `json:"contracts"`
	OriginalRecipes map[uuid.UUID]review.ArtifactRef `json:"original_recipes"`
	Semantic        []OperatorSemanticPrerequisite   `json:"semantic_prerequisites"`
	Masks           []OperatorInteractiveMask        `json:"interactive_masks"`
}
type OperatorCaseRuntime struct {
	MixedChildren                                                                *v2qualification.OperatorMixedChildren `json:"-"`
	Repository, Directory, ObjectsDirectory, ScratchDirectory, ApprovalDirectory string
	Python, Script, PythonSHA, ScriptSHA, InventorySHA                           string
	Inventory                                                                    json.RawMessage
	Installation, Policy                                                         json.RawMessage
	ObjectBytesLimit                                                             int64
	MaxTicks                                                                     int
}
type OperatorCaseTarget struct {
	Item        uuid.UUID          `json:"item_id"`
	Asset       uuid.UUID          `json:"asset_id"`
	Version     uuid.UUID          `json:"asset_version_id"`
	QualityTask uuid.UUID          `json:"quality_task_id"`
	Target      review.ArtifactRef `json:"target"`
	Quality     review.ArtifactRef `json:"quality_report"`
}
type OperatorCaseMeasured struct {
	Schema           int                        `json:"schema_version"`
	Boundary         string                     `json:"boundary"`
	CaseID           string                     `json:"case_id"`
	CorpusSHA        string                     `json:"corpus_sha256"`
	Evidence         json.RawMessage            `json:"evidence"`
	EffectiveStyle   json.RawMessage            `json:"effective_style"`
	Targets          []OperatorCaseTarget       `json:"targets"`
	Objects          []operatorObjectDescriptor `json:"objects"`
	FinalComposition []OperatorFinalComposition `json:"final_composition"`
}

func operatorCasePlan(inputs []applyInput) (json.RawMessage, []ApplyV4Stage, error) {
	if len(inputs) < 1 || len(inputs) > 5000 {
		return nil, nil, errors.New("operator collection size invalid")
	}
	var cfg struct {
		StyleConfig, OverrideConfig, JobOverrides, GraphStyleConfig json.RawMessage
		AnalysisDecisionSHA                                         string
	}
	// Config uses the actual production wire keys, not Go field names.
	var wire struct {
		StyleConfig         json.RawMessage `json:"style_config"`
		OverrideConfig      json.RawMessage `json:"override_config"`
		JobOverrides        json.RawMessage `json:"job_overrides"`
		GraphStyleConfig    json.RawMessage `json:"graph_style_config"`
		AnalysisDecisionSHA string          `json:"analysis_decision_sha"`
	}
	if json.Unmarshal(inputs[0].Config, &wire) != nil {
		return nil, nil, errors.New("operator actual Apply config invalid")
	}
	cfg.StyleConfig, cfg.OverrideConfig, cfg.JobOverrides, cfg.GraphStyleConfig, cfg.AnalysisDecisionSHA = wire.StyleConfig, wire.OverrideConfig, wire.JobOverrides, wire.GraphStyleConfig, wire.AnalysisDecisionSHA
	for _, item := range inputs {
		if !reflect.DeepEqual(item.Config, inputs[0].Config) {
			return nil, nil, errors.New("operator collection config differs")
		}
	}
	effective, e := EffectiveApplyV4Style(cfg.StyleConfig, cfg.JobOverrides)
	if e != nil {
		return nil, nil, e
	}
	effective, e = EffectiveApplyV4Style(effective, cfg.OverrideConfig)
	if e != nil {
		return nil, nil, e
	}
	if len(cfg.GraphStyleConfig) != 0 {
		if cfg.AnalysisDecisionSHA == "" {
			return nil, nil, errors.New("operator orphan graph style")
		}
		effective, e = canonical.Bytes(cfg.GraphStyleConfig)
		if e != nil {
			return nil, nil, e
		}
	}
	plan, e := CompileApplyV4Plan(effective, len(inputs))
	if e != nil {
		return nil, nil, e
	}
	plan, e = editorPrecomposeStages(inputs[0].Config, plan)
	return effective, plan, e
}
func RunOperatorRendererCase(ctx context.Context, spec OperatorCaseSpec, runtime OperatorCaseRuntime) (OperatorCaseMeasured, error) {
	fail := func() (OperatorCaseMeasured, error) {
		return OperatorCaseMeasured{}, errors.New("operator renderer case scope invalid")
	}
	if _, bounded := ctx.Deadline(); !bounded {
		return fail()
	}
	if spec.Schema != 1 || spec.ID == "" || spec.Tenant == uuid.Nil || spec.Run == uuid.Nil || spec.Actor == uuid.Nil || len(spec.CorpusSHA) != 64 {
		return fail()
	}
	for _, dir := range []string{runtime.Directory, runtime.ObjectsDirectory, runtime.ScratchDirectory, runtime.ApprovalDirectory} {
		if e := operatorPrivateDirectory(runtime.Repository, dir); e != nil {
			return OperatorCaseMeasured{}, e
		}
		relative, e := filepath.Rel(runtime.Directory, dir)
		if e != nil || relative == ".." || len(relative) > 3 && relative[:3] == "../" {
			return fail()
		}
	}
	digest := sha256.Sum256(runtime.Inventory)
	if hex.EncodeToString(digest[:]) != runtime.InventorySHA {
		return fail()
	}
	var inventory struct {
		Rows []struct {
			Operation string   `json:"operation"`
			Roles     []string `json:"roles"`
			Available bool     `json:"available"`
		} `json:"rows"`
	}
	if json.Unmarshal(runtime.Inventory, &inventory) != nil {
		return fail()
	}
	allowed := map[string]map[string]bool{}
	for _, row := range inventory.Rows {
		if !row.Available {
			continue
		}
		if _, exists := allowed[row.Operation]; exists {
			return fail()
		}
		roles := map[string]bool{}
		for _, role := range row.Roles {
			if role == "" || roles[role] {
				return fail()
			}
			roles[role] = true
		}
		allowed[row.Operation] = roles
	}
	objects, e := NewOperatorPhysicalObjects(runtime.Repository, runtime.ObjectsDirectory, allowed, runtime.ObjectBytesLimit)
	if e != nil {
		return OperatorCaseMeasured{}, e
	}
	retained := map[uuid.UUID]styleArtifact{}
	total := int64(0)
	for _, a := range spec.Objects {
		if a.ArtifactID == uuid.Nil || a.Key != a.ArtifactID.String() || a.Bucket != "operator-measurement" || a.VersionID != a.SHA256 || a.Bytes < 1 || a.Bytes > runtime.ObjectBytesLimit-total {
			return fail()
		}
		if _, duplicate := retained[a.ArtifactID]; duplicate {
			return fail()
		}
		f, e := objects.Open(ctx, a.object())
		if e != nil {
			return OperatorCaseMeasured{}, e
		}
		hash := sha256.New()
		n, readErr := io.Copy(hash, io.LimitReader(f, a.Bytes+1))
		closeErr := f.Close()
		if readErr != nil {
			return OperatorCaseMeasured{}, readErr
		}
		if closeErr != nil {
			return OperatorCaseMeasured{}, closeErr
		}
		if n != a.Bytes || hex.EncodeToString(hash.Sum(nil)) != a.SHA256 {
			return fail()
		}
		total += n
		retained[a.ArtifactID] = a
	}
	inputs, e := cloneOperator(spec.Sources)
	if e != nil {
		return OperatorCaseMeasured{}, e
	}
	seen := map[uuid.UUID]bool{}
	for i := range inputs {
		item := &inputs[i]
		if seen[item.ItemID] || item.Ordinal != i {
			return fail()
		}
		seen[item.ItemID] = true
		actual, ok := retained[item.Image.ArtifactID]
		if !ok || actual != item.Image {
			return fail()
		}
		if item.Style != nil {
			actual, ok = retained[item.Style.ArtifactID]
			if !ok || actual != *item.Style {
				return fail()
			}
		}
		item.Working, e = readWorking(ctx, objects, "operator-measurement", item.Image)
		if e != nil {
			return OperatorCaseMeasured{}, e
		}
		item.Sensor, e = parseApplySensorAlignment(item.Metadata, item.Working)
		if e != nil {
			return OperatorCaseMeasured{}, e
		}
		for _, ref := range refsForImage(item.Image.ref(), item.Working) {
			a, ok := retained[ref.ID]
			if !ok || a.SHA256 != ref.SHA256 {
				return fail()
			}
		}
	}
	effective, plan, e := operatorCasePlan(inputs)
	if e != nil {
		return OperatorCaseMeasured{}, e
	}
	if len(plan) == 0 {
		return fail()
	}
	lease := ApplyLease{TenantID: spec.Tenant, RunID: spec.Run, SourceCollectionID: uuid.NewSHA1(spec.Run, []byte("operator-inputs/"+spec.CorpusSHA)), Token: uuid.New(), Phase: plan[0].Phase, ItemCount: len(inputs), GraphVersion: 4}
	ledger, e := NewOperatorApplyLedger(lease, inputs, plan, nil)
	if e != nil {
		return OperatorCaseMeasured{}, e
	}
	ledger.mixedChildren, ledger.mixedCorpus = runtime.MixedChildren, spec.CorpusSHA
	for id, a := range retained {
		ledger.objects[id] = a
	}
	fileReviews := &OperatorFileReviews{Ledger: ledger, Objects: objects, Directory: runtime.ApprovalDirectory, Actor: spec.Actor, Snapshot: uuid.NewSHA1(spec.Run, []byte("operator-snapshot/"+spec.CorpusSHA)), SnapshotDigest: spec.CorpusSHA}
	ledger.decisionPort = fileReviews
	compute := &OperatorProcessCompute{Repository: runtime.Repository, Python: runtime.Python, Script: runtime.Script, Directory: runtime.Directory, Scratch: runtime.ScratchDirectory, Installation: runtime.Installation, Policy: runtime.Policy, InventorySHA: runtime.InventorySHA, PythonSHA: runtime.PythonSHA, ScriptSHA: runtime.ScriptSHA, Objects: objects, Ledger: ledger, InputContracts: spec.Contracts}
	prerequisites := []OperatorMeasuredTask{}
	semanticItems := map[uuid.UUID]bool{}
	for _, request := range spec.Semantic {
		if semanticItems[request.Item] {
			return fail()
		}
		semanticItems[request.Item] = true
		measured, err := MeasureSemanticPrerequisites(ctx, ledger, objects, compute, request)
		if err != nil {
			return OperatorCaseMeasured{}, err
		}
		prerequisites = append(prerequisites, measured...)
	}
	approvals := []OperatorMaskApproval{}
	for _, request := range spec.Masks {
		measured, _, err := MeasureInteractiveMask(ctx, ledger, objects, compute, request)
		if err != nil {
			return OperatorCaseMeasured{}, err
		}
		approval, err := ApproveOperatorMask(ctx, request, measured, ledger, objects, spec.Actor, runtime.ApprovalDirectory)
		if err != nil {
			return OperatorCaseMeasured{}, err
		}
		approvals = append(approvals, approval)
		prerequisites = append(prerequisites, measured)
	}
	if e = bindOperatorMaskInputs(ledger, approvals); e != nil {
		return OperatorCaseMeasured{}, e
	}
	ledger.maskApprovals = approvals
	coordinator := ApplyCoordinator{Store: ledger, Publisher: OperatorDocumentPublisher{Objects: objects, Ledger: ledger}, Objects: objects, Bucket: "operator-measurement"}
	observations, e := MeasureOperatorApply(ctx, coordinator, ledger, compute, runtime.MaxTicks)
	if e != nil {
		return OperatorCaseMeasured{}, e
	}
	observations = append(prerequisites, observations...)
	evidence, e := FreezeOperatorMeasurement(ctx, ledger, objects, observations)
	if e != nil {
		return OperatorCaseMeasured{}, e
	}
	targets := []OperatorCaseTarget{}
	seen = map[uuid.UUID]bool{}
	for id, node := range ledger.plans {
		if node.Operation != "quality.check" {
			continue
		}
		var p struct {
			Target review.ArtifactRef `json:"target"`
			Kind   string             `json:"target_kind"`
		}
		if json.Unmarshal(node.Params, &p) != nil || p.Kind != "working" || seen[node.SourceItemID] {
			return fail()
		}
		seen[node.SourceItemID] = true
		target, ok := ledger.objects[p.Target.ID]
		if !ok || target.SHA256 != p.Target.SHA256 {
			return fail()
		}
		if _, e = readWorking(ctx, objects, "operator-measurement", target); e != nil {
			return OperatorCaseMeasured{}, e
		}
		count := 0
		var report review.ArtifactRef
		for _, proof := range ledger.outputs[id] {
			if proof.Role == "quality_report" {
				count++
				report = proof.Artifact.ref()
			}
		}
		if count != 1 {
			return fail()
		}
		targets = append(targets, OperatorCaseTarget{Item: node.SourceItemID, Asset: node.SourceAssetID, Version: node.SourceVersionID, QualityTask: id, Target: p.Target, Quality: report})
	}
	if len(targets) != len(inputs) {
		return fail()
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].Item.String() < targets[j].Item.String() })
	final, e := compileOperatorFinalCompositions(ctx, spec, ledger, objects, targets, effective, evidence)
	if e != nil {
		return OperatorCaseMeasured{}, e
	}
	descriptors, e := compute.descriptors(PreparedTask{})
	if e != nil {
		return OperatorCaseMeasured{}, e
	}
	return OperatorCaseMeasured{Schema: 1, Boundary: "complete_local_apply_measurement_not_qualified", CaseID: spec.ID, CorpusSHA: spec.CorpusSHA, Evidence: evidence, EffectiveStyle: effective, Targets: targets, Objects: descriptors, FinalComposition: final}, nil
}
