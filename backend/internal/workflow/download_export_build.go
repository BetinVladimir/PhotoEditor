package workflow

import (
	"context"
 "bytes"
	"crypto/sha256"
	"encoding/hex"
 "encoding/json"
	"errors"

	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
)

type DownloadExportPlanRepository struct{ DB artifactjson.RowQuerier }

// Register persists the compiler output before any parameter object is written.
// Every row is immutable and an exact replay is the only accepted conflict.
func (r DownloadExportPlanRepository) Register(ctx context.Context, tenant uuid.UUID, batch DownloadExportBatch) ([]CompiledDownloadNode, error) {
	if r.DB == nil || tenant == uuid.Nil || batch.Spec.RunID == uuid.Nil {
		return nil, errors.New("invalid download export repository")
	}
	registry, err := LoadRegistry()
	if err != nil {
		return nil, err
	}
	nodes, err := CompileDownloadExports(batch.Spec, registry)
	if err != nil {
		return nil, err
	}
	for _, node := range nodes {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		sum := sha256.Sum256(node.Params)
		inputs, marshalErr := canonical.Marshal(node.Inputs)
		if marshalErr != nil {
			return nil, marshalErr
		}
		var saved bool
		err = r.DB.QueryRow(ctx, "SELECT photo.register_download_export_plan($1,$2,$3,$4,$5,$6,$7,$8)",
			tenant, batch.Spec.RunID, node.ID, node.Ordinal, node.Key, inputs, hex.EncodeToString(sum[:]), []byte(node.Params)).Scan(&saved)
		if err != nil {
			return nil, err
		}
		if !saved {
			return nil, errors.New("download export plan stale or conflicting")
		}
	}
	return nodes, nil
}

type PublishedDownloadExport struct {
	Node   CompiledDownloadNode
	Params review.ArtifactRef
}

type DownloadExportBuilder struct {
	Plans     DownloadExportPlanRepository
	Publisher ContractPublisher
}

func (b DownloadExportBuilder) Publish(ctx context.Context, tenant uuid.UUID, batch DownloadExportBatch) ([]PublishedDownloadExport, error) {
	if b.Publisher == nil {
		return nil, errors.New("download export publisher required")
	}
	nodes, err := b.Plans.Register(ctx, tenant, batch)
	if err != nil {
		return nil, err
	}
	out := make([]PublishedDownloadExport, 0, len(nodes))
	for _, node := range nodes {
		ref, publishErr := b.Publisher.Publish(ctx, artifactjson.Scope{Tenant: tenant, Run: batch.Spec.RunID, Owner: node.ID, Purpose: "task_params"}, node.Params)
		if publishErr != nil {
			return nil, publishErr
		}
		sum := sha256.Sum256(node.Params)
		if ref.SHA256 != hex.EncodeToString(sum[:]) {
			return nil, errors.New("download params publication integrity failure")
		}
		out = append(out, PublishedDownloadExport{Node: node, Params: review.ArtifactRef{ID: ref.ID, SHA256: ref.SHA256}})
	}
	return out, nil
}

func PrepareDownloadExportTask(entry PublishedDownloadExport, sizes map[uuid.UUID]int64, item DownloadExportItem) (PreparedTask, error) {
	fail := func() (PreparedTask, error) { return PreparedTask{}, errors.New("invalid download export task") }
	n := entry.Node
	if n.Operation != "export.encode" || n.Version != "1" || n.ResourceClass != "cpu" || n.Ordinal != item.Ordinal || !validExpansionRef(entry.Params) || len(n.Inputs) < 2 || len(n.Inputs) > 3 {
		return fail()
	}
	sum := sha256.Sum256(n.Params)
	if entry.Params.SHA256 != hex.EncodeToString(sum[:]) {
		return fail()
	}
 if item.Image.Canvas.Width<1 || item.Image.Canvas.Height<1 || item.Image.Canvas.Width>200000000 || item.Image.Canvas.Height>200000000 || item.Image.DType!="float32" { return fail() }
 if !validExpansionRef(item.Manifest) || !validExpansionRef(item.Image.Pixels) || item.Manifest==item.Image.Pixels { return fail() }
 if item.Image.Color.ProfileID!="work_rec2020_gamma24_v1" || item.Image.Color.Encoding!="gamma24" { return fail() }
 if item.Image.Alpha!=nil && (!validExpansionRef(*item.Image.Alpha) || *item.Image.Alpha==item.Manifest || *item.Image.Alpha==item.Image.Pixels) { return fail() }
 if n.Inputs[0]!=item.Manifest || n.Inputs[1]!=item.Image.Pixels || len(n.Inputs)!=2+boolInt(item.Image.Alpha!=nil) { return fail() }
 if item.Image.Alpha!=nil && n.Inputs[2]!=*item.Image.Alpha { return fail() }
 pixels := item.Image.Canvas.Width * item.Image.Canvas.Height
	if pixels < 1 || pixels > 200000000 {
		return fail()
	}
	inputBytes := int64(0)
	for _, ref := range n.Inputs {
		value, ok := sizes[ref.ID]
		if !ok || value < 1 || value > 1<<53-1 {
			return fail()
		}
		inputBytes += value
	}
 var params encodeParams
 if len(n.Params)<2 || len(n.Params)>1048576 || json.Unmarshal(n.Params, &params) != nil || params.Image!=item.Manifest || domainDigestInvalid(params.ICCSHA256) { return fail() }
 canonicalParams,err:=canonical.Marshal(params)
 if err!=nil || !bytes.Equal(canonicalParams,n.Params) { return fail() }
 preset := DownloadPreset{Format:params.Format, ICCID:params.ICCID, Size:params.Size, Quality:params.Quality, Subsampling:params.Subsampling, Alpha:params.Alpha, MatteRGB:params.MatteRGB, MetadataPolicy:params.MetadataPolicy}
 if !validDownloadPreset(preset) { return fail() }
 width,height := encodeDimensions(item.Image.Canvas.Width,item.Image.Canvas.Height,params.Size)
 targetPixels := width*height
 if width<1 || height<1 || targetPixels<1 || targetPixels>200000000 { return fail() }
 ram := encodeRAMBytes(pixels,targetPixels,item.Image.Alpha!=nil)
 output := targetPixels*8 + 2*1048576
	scratch := inputBytes + int64(len(n.Params)) + output*2
	profile, err := canonical.Marshal(struct {
		Backend   string           `json:"backend"`
		Precision string           `json:"precision"`
		MaxPixels int64            `json:"max_pixels"`
		Estimate  ResourceEstimate `json:"resource_estimate"`
	}{"cpu", "float32", 200000000, ResourceEstimate{RAMBytes: ram, ScratchBytes: scratch, OutputBytes: output, PixelCount: targetPixels}})
	if err != nil {
		return fail()
	}
	if _, err = ParseResourceEstimate(profile); err != nil {
		return fail()
	}
	digest, err := repairInputDigest(n.Operation, n.Version, entry.Params, n.Inputs)
	if err != nil {
		return fail()
	}
	return PreparedTask{ID: n.ID, NodeKey: n.Key, Operation: n.Operation, Version: n.Version, ResourceClass: n.ResourceClass,
		TimeoutSeconds: n.TimeoutSeconds, Params: entry.Params, Inputs: append([]review.ArtifactRef(nil), n.Inputs...), InputDigest: digest, ExecutionProfile: profile}, nil
}

func (r DownloadExportPlanRepository) Materialize(ctx context.Context, tenant, run uuid.UUID, task PreparedTask) (bool, error) {
	if r.DB == nil || tenant == uuid.Nil || run == uuid.Nil || task.Operation != "export.encode" || !validExpansionRef(task.Params) {
		return false, errors.New("invalid download export materialization")
	}
	digest, err := repairInputDigest(task.Operation, task.Version, task.Params, task.Inputs)
	if err != nil || digest != task.InputDigest {
		return false, errors.New("invalid download export digest")
	}
	if _, err = ParseResourceEstimate(task.ExecutionProfile); err != nil {
		return false, err
	}
	raw, err := canonical.Marshal(task)
	if err != nil {
		return false, err
	}
	var saved bool
	err = r.DB.QueryRow(ctx, "SELECT photo.materialize_download_export_task($1,$2,$3)", tenant, run, raw).Scan(&saved)
	return saved, err
}

func (r DownloadExportPlanRepository) Seal(ctx context.Context, tenant, run uuid.UUID) (bool, error) {
	if r.DB == nil || tenant == uuid.Nil || run == uuid.Nil {
		return false, errors.New("invalid download export seal")
	}
	var sealed bool
	err := r.DB.QueryRow(ctx, "SELECT photo.seal_download_exports($1,$2)", tenant, run).Scan(&sealed)
	return sealed, err
}

type DownloadExportCoordinator struct {
	Source  DownloadExportLoader
	Builder DownloadExportBuilder
	Tasks   DownloadExportPlanRepository
}

func (c DownloadExportCoordinator) Build(ctx context.Context, tenant, run uuid.UUID) (bool, error) {
	if changed,preflightErr:=c.Tasks.FailPlanningSource(ctx,tenant,run,nil);preflightErr!=nil||changed{return changed,preflightErr}
	batch, err := c.Source.Load(ctx, tenant, run)
	if err != nil {
		var observation *downloadSourceFailure
		if errors.As(err,&observation){changed,saveErr:=c.Tasks.FailPlanningSource(ctx,tenant,run,observation);if saveErr!=nil{return false,saveErr};if changed{return true,nil}}
		return false, err
	}
	published, err := c.Builder.Publish(ctx, tenant, batch)
	if err != nil {
		return false, err
	}
	if len(published) != len(batch.Spec.Items) {
		return false, errors.New("incomplete download export publication")
	}
	for i, entry := range published {
		task, prepErr := PrepareDownloadExportTask(entry, batch.Sizes, batch.Spec.Items[i])
		if prepErr != nil {
			return false, prepErr
		}
		saved, saveErr := c.Tasks.Materialize(ctx, tenant, run, task)
		if saveErr != nil {
			return false, saveErr
		}
		if !saved {
			return false, errors.New("download export materialization conflict")
		}
	}
	sealed, err := c.Tasks.Seal(ctx, tenant, run)
	if err != nil {
		return false, err
	}
	if !sealed {
		return false, errors.New("download export barrier incomplete")
	}
	return true, nil
}
