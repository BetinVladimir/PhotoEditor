package v2render

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

var ErrAuthoredGeometryPending = errors.New("AUTHORED_GEOMETRY_CANONICAL_PENDING")

type authoredGeometryProducerObject struct {
	Ref     v2input.Ref `json:"ref"`
	Role    string      `json:"role"`
	Bucket  string      `json:"bucket"`
	Key     string      `json:"object_key"`
	Version string      `json:"object_version"`
	Bytes   int64       `json:"bytes"`
	Media   string      `json:"media_type"`
}
type authoredGeometryProducerObservation struct {
	Context AuthoredInstructionSource `json:"context"`
	Scope   struct {
		Root v2input.Ref `json:"root"`
	} `json:"scope"`
	RootJCS       string                           `json:"root_jcs"`
	MappingJCS    string                           `json:"mapping_jcs"`
	Producer      AuthoredPhaseTask                `json:"producer"`
	Native        AuthoredPhaseTask                `json:"native"`
	Objects       []authoredGeometryProducerObject `json:"objects"`
	NativeObjects []authoredGeometryProducerObject `json:"native_objects"`
}
type CurrentAuthoredGeometry struct {
	task           AuthoredPhaseTask
	result, report v2input.Ref
	output         v2input.SessionVersionBinding
	native         AuthoredGeometryPhaseWitness
	closure        []v2input.Ref
	proof          []byte
}
type AuthoredGeometryProducerReader struct {
	Scheduler *pgxpool.Pool
	Objects   v2input.SessionAnalysisObjectReader
	Bucket    string
}

func (r AuthoredGeometryProducerReader) Read(ctx context.Context, source AuthoredInstructionSource) (CurrentAuthoredGeometry, error) {
	zero := CurrentAuthoredGeometry{}
	if e := ctx.Err(); e != nil {
		return zero, e
	}
	if r.Scheduler == nil || r.Objects == nil || r.Bucket == "" || source.Instruction.Kind != "geometry" {
		return zero, v2input.ErrInvalid
	}
	read := func() ([]byte, error) {
		var raw []byte
		e := database.TenantTransaction(ctx, r.Scheduler, source.Tenant, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT photo.read_authored_render_geometry_producer($1,$2,$3)`, source.Tenant, source.Parent, source.Instruction.Ordinal).Scan(&raw)
		})
		return raw, e
	}
	before, e := read()
	if e != nil {
		return zero, e
	}
	if len(before) == 0 {
		return zero, ErrAuthoredGeometryPending
	}
	var observed authoredGeometryProducerObservation
	if json.Unmarshal(before, &observed) != nil || len(observed.Objects) < 1 || len(observed.Objects) > 128 {
		return zero, v2input.ErrInvalid
	}
	a, e := canonical.Marshal(observed.Context)
	if e != nil {
		return zero, e
	}
	b, e := canonical.Marshal(source)
	if e != nil || !bytes.Equal(a, b) {
		return zero, v2input.ErrExpired
	}
	documents := map[v2input.Ref][]byte{}
	roles := map[v2input.Ref]string{}
	seen := map[uuid.UUID]bool{}
	if len(observed.Producer.Outputs) != len(observed.Objects) {
		return zero, v2input.ErrInvalid
	}
	for i, p := range observed.Objects {
		if e := ctx.Err(); e != nil {
			return zero, e
		}
		if !validLayerArtifactRef(p.Ref) || seen[p.Ref.ArtifactID] || p.Ref != observed.Producer.Outputs[i] || p.Bucket != r.Bucket || p.Key == "" || p.Version == "" || p.Version == "null" || p.Bytes < 1 {
			return zero, v2input.ErrInvalid
		}
		seen[p.Ref.ArtifactID] = true
		roles[p.Ref] = p.Role
		pin := objectstore.Object{Key: p.Key, VersionID: p.Version, SHA256: p.Ref.SHA256, Bytes: p.Bytes, ContentType: p.Media}
		switch p.Role {
		case "authored_phase", "authored_mask_mapping", "document", "working_image", "report", "mask":
			if p.Media != "application/json" || p.Bytes > 8388608 {
				return zero, v2input.ErrInvalid
			}
			raw, e := v2input.ReadPinnedSemanticDocument(ctx, r.Objects, pin, p.Ref)
			if e != nil {
				return zero, e
			}
			wire, e := canonical.Bytes(raw)
			if e != nil || !bytes.Equal(wire, raw) {
				return zero, v2input.ErrInvalid
			}
			documents[p.Ref] = raw
		case "pixels", "alpha", "mask_pixels":
			if e := verifyAuthoredFinalBinary(ctx, r.Objects, pin, p.Ref, p.Role); e != nil {
				return zero, e
			}
		default:
			return zero, v2input.ErrInvalid
		}
	}
	if len(observed.NativeObjects) != 4 {
		return zero, v2input.ErrInvalid
	}
	for _, p := range observed.NativeObjects {
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		if !validLayerArtifactRef(p.Ref) || p.Bucket != r.Bucket || p.Key == "" || p.Version == "" || p.Version == "null" || p.Media != "application/json" || p.Bytes < 2 || p.Bytes > 8388608 {
			return zero, v2input.ErrInvalid
		}
		raw, err := v2input.ReadPinnedSemanticDocument(ctx, r.Objects, objectstore.Object{Key: p.Key, VersionID: p.Version, SHA256: p.Ref.SHA256, Bytes: p.Bytes, ContentType: p.Media}, p.Ref)
		if err != nil {
			return zero, err
		}
		wire, err := canonical.Bytes(raw)
		if err != nil || !bytes.Equal(wire, raw) {
			return zero, v2input.ErrInvalid
		}
		if old, ok := documents[p.Ref]; ok && !bytes.Equal(old, raw) {
			return zero, v2input.ErrInvalid
		}
		documents[p.Ref] = raw
	}
	current, e := validateAuthoredGeometryProducerDocuments(source, observed, documents, roles)
	if e != nil {
		return zero, e
	}
	after, e := read()
	if e != nil {
		return zero, e
	}
	if !bytes.Equal(before, after) {
		return zero, v2input.ErrExpired
	}
	if e = ctx.Err(); e != nil {
		return zero, e
	}
	current.proof = append([]byte(nil), after...)
	return current, nil
}

func validateAuthoredGeometryProducerDocuments(source AuthoredInstructionSource, o authoredGeometryProducerObservation, docs map[v2input.Ref][]byte, roles map[v2input.Ref]string) (CurrentAuthoredGeometry, error) {
	bad := func() (CurrentAuthoredGeometry, error) { return CurrentAuthoredGeometry{}, v2input.ErrInvalid }
	root := o.Scope.Root
	if roles[root] != "authored_phase" || !bytes.Equal(docs[root], []byte(o.RootJCS)) {
		return bad()
	}
	if _, e := v2draft.DocumentChildren(docs[root], "AuthoredGeometryPhaseResultV3", root); e != nil {
		return bad()
	}
	var body struct {
		Tenant      uuid.UUID                     `json:"tenant_id"`
		Parent      uuid.UUID                     `json:"parent_run_id"`
		Instruction uuid.UUID                     `json:"instruction_id"`
		Program     string                        `json:"program_sha256"`
		Recipe      v2input.Ref                   `json:"recipe"`
		Input       v2input.SessionVersionBinding `json:"input_source"`
		Result      v2input.Ref                   `json:"blended_result"`
		Image       v2input.Ref                   `json:"blended_image"`
		Mapping     v2input.Ref                   `json:"mask_mapping"`
		Native      struct {
			Run       uuid.UUID   `json:"run_id"`
			Task      uuid.UUID   `json:"task_id"`
			Result    uuid.UUID   `json:"result_id"`
			Attempt   int         `json:"attempt"`
			Fence     int64       `json:"fence"`
			Operation string      `json:"operation"`
			Version   string      `json:"operation_version"`
			Params    v2input.Ref `json:"params"`
			Root      v2input.Ref `json:"root"`
			Image     v2input.Ref `json:"image"`
			Report    v2input.Ref `json:"report"`
		} `json:"native"`
	}
	if json.Unmarshal(docs[root], &body) != nil || body.Tenant != source.Tenant || body.Parent != source.Parent || body.Instruction != source.Instruction.Identity || body.Program != source.ProgramSHA || body.Recipe != source.OriginalRecipe || body.Input != source.Source {
		return bad()
	}
	if roles[body.Mapping] != "authored_mask_mapping" || !bytes.Equal(docs[body.Mapping], []byte(o.MappingJCS)) {
		return bad()
	}
	if _, e := v2draft.DocumentChildren(docs[body.Mapping], "AuthoredGeometryMaskMappingV2", body.Mapping); e != nil {
		return bad()
	}
	n := body.Native
	task := o.Native
	if n.Run != task.RunID || n.Task != task.TaskID || n.Result != task.ResultID || n.Attempt != task.Attempt || n.Fence != task.Fence || n.Operation != task.Operation || n.Version != task.Version || n.Params != task.Params {
		return bad()
	}
	if len(o.NativeObjects) != 4 {
		return bad()
	}
	for i, ref := range []v2input.Ref{n.Root, n.Image, n.Report, n.Params} {
		if o.NativeObjects[i].Ref != ref {
			return bad()
		}
	}
	rootFound := false
	for _, output := range task.Outputs {
		if output == n.Root {
			rootFound = true
		}
	}
	if !rootFound || !validLayerArtifactRef(n.Image) || !validLayerArtifactRef(n.Report) {
		return bad()
	}
	// Actual optical tasks canonically emit ImageResult root only. Decode the
	// physically pinned root to bind nested refs; upscale emits WorkingImage.
	if task.Operation == "image.upscale" {
		var params struct {
			Image v2input.Ref `json:"image"`
		}
		var report struct {
			Image v2input.Ref `json:"image"`
		}
		if json.Unmarshal(docs[n.Params], &params) != nil || json.Unmarshal(docs[n.Report], &report) != nil || params.Image != source.Source.Source || report.Image != params.Image {
			return bad()
		}
		if n.Root != n.Image {
			return bad()
		}
		if _, err := v2draft.DocumentChildren(docs[n.Report], "UpscaleReport", n.Report); err != nil {
			return bad()
		}
	} else {
		nativeResult, err := v2draft.ParseImageResult(docs[n.Root], n.Root)
		if err != nil || nativeResult.WorkingImage != n.Image || nativeResult.Report != n.Report {
			return bad()
		}
		if _, err := v2draft.DocumentChildren(docs[n.Report], "ImageReport", n.Report); err != nil {
			return bad()
		}
	}
	if _, err := review.ParseWorkingImage(docs[n.Image]); err != nil {
		return bad()
	}
	var rootFields, mappingFields map[string]json.RawMessage
	if json.Unmarshal(docs[root], &rootFields) != nil || json.Unmarshal(docs[body.Mapping], &mappingFields) != nil {
		return bad()
	}
	for _, pair := range [][2]string{{"tenant_id", "tenant_id"}, {"parent_run_id", "parent_run_id"}, {"instruction_id", "instruction_id"}, {"program_sha256", "program_sha256"}, {"recipe", "recipe"}, {"input_source", "source"}, {"native", "native"}} {
		if !bytes.Equal(rootFields[pair[0]], mappingFields[pair[1]]) {
			return bad()
		}
	}
	for key, expected := range map[string]any{"producer_run_id": o.Producer.RunID, "producer_task_id": o.Producer.TaskID, "attempt": o.Producer.Attempt, "fence": o.Producer.Fence} {
		value, err := canonical.Marshal(expected)
		if err != nil || !bytes.Equal(value, mappingFields[key]) {
			return bad()
		}
	}
	result, e := v2draft.ParseImageResult(docs[body.Result], body.Result)
	if e != nil || result.WorkingImage != body.Image || roles[body.Result] != "document" || roles[body.Image] != "working_image" || roles[result.Report] != "report" {
		return bad()
	}
	image, e := review.ParseWorkingImage(docs[body.Image])
	if e != nil {
		return bad()
	}
	ref := func(v review.ArtifactRef) v2input.Ref { return v2input.Ref{ArtifactID: v.ID, SHA256: v.SHA256} }
	if roles[ref(image.Pixels)] != "pixels" || (image.Alpha != nil && roles[ref(*image.Alpha)] != "alpha") {
		return bad()
	}
	if _, e = v2draft.DocumentChildren(docs[result.Report], "ImageReport", result.Report); e != nil {
		return bad()
	}
	var mapping struct {
		Target      v2input.Ref     `json:"target_image"`
		Canvas      json.RawMessage `json:"target_canvas"`
		Chain       json.RawMessage `json:"target_chain"`
		Ordinal     int             `json:"ordinal"`
		Predecessor string          `json:"predecessor_sha256"`
	}
	if json.Unmarshal(docs[body.Mapping], &mapping) != nil || mapping.Target != body.Image || mapping.Ordinal != source.Instruction.Ordinal || mapping.Predecessor != source.PredecessorSHA {
		return bad()
	}
	a, e := canonical.Marshal(image.Canvas)
	if e != nil || !bytes.Equal(a, mapping.Canvas) {
		return bad()
	}
	b, e := canonical.Marshal(image.TransformChain)
	if e != nil || !bytes.Equal(b, mapping.Chain) {
		return bad()
	}
	for _, item := range []struct {
		ref      *v2input.Ref
		selector string
	}{{&result.ChangedMask, "changed"}, {result.GeneratedMask, "generated"}} {
		if item.ref == nil {
			continue
		}
		if roles[*item.ref] != "mask" {
			return bad()
		}
		mask, e := review.ParseMaskRef(docs[*item.ref])
		if e != nil || mask.Selector != item.selector || mask.Protected || mask.Confidence != 1 || mask.ObjectID != nil || mask.ModelLockID != nil || mask.Canvas != image.Canvas || roles[ref(mask.Artifact)] != "mask_pixels" {
			return bad()
		}
		chain, e := canonical.Marshal(mask.TransformChain)
		if e != nil || !bytes.Equal(chain, b) {
			return bad()
		}
	}
	canvas, e := canonical.Hash(image.Canvas)
	if e != nil {
		return bad()
	}
	witness := AuthoredGeometryPhaseWitness{Task: task, Root: n.Root, Image: n.Image, Report: n.Report}
	output := v2input.SessionVersionBinding{AssetVersionID: source.Source.AssetVersionID, Source: body.Image, CanvasHash: canvas}
	// The actual root3 is retained as result; its blended ImageResult is a distinct output.
	if _, _, e = CompileAuthoredPhaseReceipt(source, o.Producer, output, root, result.Report, &witness, o.Producer.Outputs); e != nil {
		return bad()
	}
	return CurrentAuthoredGeometry{task: o.Producer, result: root, report: result.Report, output: output, native: witness, closure: append([]v2input.Ref(nil), o.Producer.Outputs...)}, nil
}
