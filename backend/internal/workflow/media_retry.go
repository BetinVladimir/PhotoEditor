package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
 "time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/media"
	"photoeditor/backend/internal/review"
)

type MediaMaster struct {
	Artifact  review.ArtifactRef
	Bytes     int64
	Width     int64
	Height    int64
	MediaType string
	Working   *review.WorkingImage
}

type MediaRetryDocument struct {
	ID, EventID, AssetVersionID             uuid.UUID
	Variant, Operation, Suffix, ProfileHash string
	Params                                  json.RawMessage
	Inputs                                  []review.ArtifactRef
}

type PublishedMediaRetry struct {
	Document MediaRetryDocument
	Params   review.ArtifactRef
}

type MediaRetryPlanStore interface {
	Claim(context.Context) (*MediaRetryLease, error)
	Release(context.Context, MediaRetryLease) (bool, error)
	Begin(context.Context, MediaRetryLease, uuid.UUID, string, json.RawMessage) (bool, error)
	Register(context.Context, MediaRetryLease, uuid.UUID, MediaRetryDocument, string) (bool, error)
	Materialize(context.Context, MediaRetryLease, uuid.UUID, PreparedTask) (bool, error)
	Seal(context.Context, MediaRetryLease, uuid.UUID) (bool, error)
}

type MediaRetryCoordinator struct {
	Store     MediaRetryPlanStore
	Publisher ContractPublisher
	SRGBSHA   string
}

func (c MediaRetryCoordinator) ProcessOne(ctx context.Context) (bool, error) { return c.Process(ctx) }

// Process compiles one claimed retry into a sealed run. Node registration
// precedes parameter publication so the reservation function can authorize the
// exact expected digest. Every identity is derived from the durable event ID,
// which makes the sequence replayable after any process or object-store fault.
func (c MediaRetryCoordinator) Process(ctx context.Context) (bool, error) {
 if err:=ctx.Err();err!=nil{return false,err}
	if c.Store == nil || c.Publisher == nil {
		return false, errors.New("media retry coordinator unavailable")
	}
	lease, err := c.Store.Claim(ctx)
	if err != nil || lease == nil {
		return false, err
	}
	release := true
	defer func() {
		if release {
			cleanup,cancel:=context.WithTimeout(context.WithoutCancel(ctx),5*time.Second)
   defer cancel()
   _, _ = c.Store.Release(cleanup, *lease)
		}
	}()
 // Reject absent/unknown policy before any durable plan or object write.
 if lease.RendererPolicy!=media.CurrentRendererPolicy && lease.RendererPolicy!=media.LegacyRendererPolicy{return false,errors.New("unknown media retry renderer policy")}
 if err=ctx.Err();err!=nil{return false,err}
	runID := uuid.NewSHA1(lease.EventID, []byte("media-retry-run"))
	config, err := canonical.Marshal(struct {
		EventID        uuid.UUID `json:"event_id"`
		AssetVersionID uuid.UUID `json:"asset_version_id"`
		Variants       []string  `json:"variants"`
		SRGBSHA256     string    `json:"srgb_sha256"`
	}{lease.EventID, lease.AssetVersionID, lease.Variants, c.SRGBSHA})
	if err != nil {
		return false, err
	}
	digest := sha256.Sum256(config)
	configHash := hex.EncodeToString(digest[:])
	ok, err := c.Store.Begin(ctx, *lease, runID, configHash, config)
	if err != nil || !ok {
		if err == nil {
			err = errors.New("media retry begin rejected")
		}
		return false, err
	}
 if err=ctx.Err();err!=nil{return false,err}
	docs, err := BuildMediaRetryDocumentsForPolicy(lease.EventID, lease.AssetVersionID, lease.Master, lease.Variants, c.SRGBSHA,lease.RendererPolicy)
	if err != nil {
		return false, err
	}
	for _, doc := range docs {
		body, err := canonical.Bytes(doc.Params)
		if err != nil {
			return false, err
		}
		sum := sha256.Sum256(body)
		paramsSHA := hex.EncodeToString(sum[:])
		if ok, err = c.Store.Register(ctx, *lease, runID, doc, paramsSHA); err != nil || !ok {
			if err == nil {
				err = errors.New("media retry node rejected")
			}
			return false, err
		}
  if err=ctx.Err();err!=nil{return false,err}
		ref, err := c.Publisher.Publish(ctx, artifactjson.Scope{Tenant: lease.TenantID, Run: runID, Owner: doc.ID, Purpose: "task_params"}, body)
		if err != nil {
			return false, err
		}
  if err=ctx.Err();err!=nil{return false,err}
		task, err := PrepareMediaRetryTask(PublishedMediaRetry{Document: doc, Params: review.ArtifactRef{ID: ref.ID, SHA256: ref.SHA256}}, lease.Master)
		if err != nil {
			return false, err
		}
		if ok, err = c.Store.Materialize(ctx, *lease, runID, task); err != nil || !ok {
			if err == nil {
				err = errors.New("media retry task rejected")
			}
			return false, err
		}
	  if err=ctx.Err();err!=nil{return false,err}
 }
	if ok, err = c.Store.Seal(ctx, *lease, runID); err != nil || !ok {
		if err == nil {
			err = errors.New("media retry seal rejected")
		}
		return false, err
	}
	release = false
 if err=ctx.Err();err!=nil{return false,err}
	return true, nil
}

func mediaSuffix(mediaType string) (string, bool) {
	value, ok := map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/tiff": ".tiff", "image/webp": ".webp"}[mediaType]
	return value, ok
}

func BuildMediaRetryDocuments(eventID, versionID uuid.UUID, master MediaMaster, groups []string, srgbSHA string) ([]MediaRetryDocument, error) {
 return BuildMediaRetryDocumentsForPolicy(eventID,versionID,master,groups,srgbSHA,media.CurrentRendererPolicy)
}

// BuildMediaRetryDocumentsForPolicy keeps immutable historical Params replayable.
func BuildMediaRetryDocumentsForPolicy(eventID, versionID uuid.UUID, master MediaMaster, groups []string, srgbSHA,policy string) ([]MediaRetryDocument, error) {
 if policy!=media.CurrentRendererPolicy && policy!=media.LegacyRendererPolicy{return nil,errors.New("unknown media retry renderer policy")}
	if eventID == uuid.Nil || versionID == uuid.Nil || master.Artifact.ID == uuid.Nil || master.Bytes < 1 {
		return nil, errors.New("invalid media retry source")
	}
	if _, err := domain.ParseDigest(master.Artifact.SHA256); err != nil {
		return nil, errors.New("invalid media retry source")
	}
	if _, err := domain.ParseDigest(srgbSHA); err != nil {
		return nil, errors.New("invalid media profile")
	}
	sourceKind, suffix, inputs := "master", "", []review.ArtifactRef{master.Artifact}
	if master.Working != nil {
		w := master.Working
		master.Width, master.Height = int64(w.Canvas.Width), int64(w.Canvas.Height)
		if w.DType != "float32" || w.Color.ProfileID != "work_rec2020_gamma24_v1" {
			return nil, errors.New("invalid working master")
		}
		if _, err := domain.ParseDigest(w.Color.ICCSHA256); err != nil {
			return nil, errors.New("invalid working master")
		}
		inputs = workingInputs(master.Artifact, *w)
		for _, ref := range inputs {
			if ref.ID == uuid.Nil {
				return nil, errors.New("invalid working master")
			}
			if _, err := domain.ParseDigest(ref.SHA256); err != nil {
				return nil, errors.New("invalid working master")
			}
		}
		sourceKind = "working"
	} else {
		var ok bool
		suffix, ok = mediaSuffix(master.MediaType)
		if !ok {
			return nil, errors.New("unsupported media master")
		}
	}
	if master.Width < 1 || master.Height < 1 || master.Width > 200000 || master.Height > 200000 || master.Width*master.Height > 200000000 {
		return nil, errors.New("invalid media retry source")
	}
	seen := map[string]bool{}
	variants := []string{}
	for _, group := range groups {
		if seen[group] || (group != "thumbnail" && group != "preview") {
			return nil, errors.New("invalid media retry variants")
		}
		seen[group] = true
		if group == "thumbnail" {
			variants = append(variants, "thumb320", "thumb640")
		} else {
			variants = append(variants, "preview1600", "preview2560")
		}
	}
	if len(groups) < 1 || len(groups) > 2 {
		return nil, errors.New("invalid media retry variants")
	}
	sort.Strings(variants)
	out := make([]MediaRetryDocument, 0, len(variants))
	for _, variant := range variants {
		profile, err := media.ProfileHashForPolicy(variant, srgbSHA,policy)
		if err != nil {
			return nil, err
		}
		body, err := canonical.Marshal(struct {
			Image       review.ArtifactRef `json:"image"`
			Variant     string             `json:"variant"`
			ProfileHash string             `json:"profile_hash"`
			SourceKind  string             `json:"source_kind"`
			Suffix      string             `json:"suffix,omitempty"`
		}{master.Artifact, variant, profile, sourceKind, suffix})
		if err != nil {
			return nil, err
		}
		op := "media.preview"
		if variant[:5] == "thumb" {
			op = "media.thumbnail"
		}
		out = append(out, MediaRetryDocument{ID: uuid.NewSHA1(eventID, []byte(variant)), EventID: eventID, AssetVersionID: versionID, Variant: variant, Operation: op, Suffix: suffix, ProfileHash: profile, Params: body, Inputs: append([]review.ArtifactRef(nil), inputs...)})
	}
	return out, nil
}

func PrepareMediaRetryTask(entry PublishedMediaRetry, master MediaMaster) (PreparedTask, error) {
	n := entry.Document
	expected := []review.ArtifactRef{master.Artifact}
	if master.Working != nil {
		expected = workingInputs(master.Artifact, *master.Working)
	}
	if n.ID == uuid.Nil || n.EventID == uuid.Nil || n.AssetVersionID == uuid.Nil || !validExpansionRef(entry.Params) || len(n.Inputs) != len(expected) {
		return PreparedTask{}, errors.New("invalid media retry task")
	}
	for i := range expected {
		if n.Inputs[i] != expected[i] {
			return PreparedTask{}, errors.New("invalid media retry task")
		}
	}
	timeout := 180
	if n.Operation == "media.preview" {
		timeout = 300
	} else if n.Operation != "media.thumbnail" {
		return PreparedTask{}, errors.New("invalid media retry operation")
	}
	profile, err := mediaPresentationProfile(master, n.Variant)
	if err != nil {
		return PreparedTask{}, err
	}
	digest, err := repairInputDigest(n.Operation, "1", entry.Params, n.Inputs)
	if err != nil {
		return PreparedTask{}, err
	}
	return PreparedTask{ID: n.ID, NodeKey: "media.retry/" + n.EventID.String() + "/" + n.Variant, Operation: n.Operation, Version: "1", ResourceClass: "media", TimeoutSeconds: timeout, Params: entry.Params, Inputs: append([]review.ArtifactRef(nil), n.Inputs...), InputDigest: digest, ExecutionProfile: profile}, nil
}

func mediaPresentationProfile(master MediaMaster, variant string) (json.RawMessage, error) {
	width, height := master.Width, master.Height
	if master.Working != nil {
		width, height = int64(master.Working.Canvas.Width), int64(master.Working.Canvas.Height)
	}
	if width < 1 || height < 1 || width > 200000 || height > 200000 {
		return nil, errors.New("invalid presentation dimensions")
	}
	pixels := width * height
	// Match worker presentation admission with a conservative target pixel bound.
	// The no-upscale target fits inside the variant edge square and source canvas.
	edges := map[string]int64{"thumb320": 320, "thumb640": 640, "preview1600": 1600, "preview2560": 2560}
	edge, ok := edges[variant]
	if !ok || pixels < 1 || pixels > 200000000 {
		return nil, errors.New("invalid presentation resource dimensions")
	}
	targetPixels := edge * edge
	if pixels < targetPixels {
		targetPixels = pixels
	}
	sourceCost := int64(24)
	if master.Working != nil && master.Working.Alpha != nil {
		sourceCost = 96
	}
	// Encoded masters can carry alpha not known in advance: reserve the alpha path.
	if master.Working == nil {
		sourceCost = 96
	}
	ram := pixels*sourceCost + targetPixels*96 + 128*1024*1024
	scratch := int64(64 * 1024 * 1024)
	if master.Working != nil {
		scratch += pixels * 12 // Downloaded RGB NPY stays owned while encoding.
		if master.Working.Alpha != nil {
			scratch += pixels * 4
		}
	} else {
		if master.Bytes < 1 || master.Bytes > 9007199254740991-scratch {
			return nil, errors.New("invalid encoded presentation size")
		}
		scratch += master.Bytes
	}
	profile, err := canonical.Marshal(struct {
		Backend   string           `json:"backend"`
		Precision string           `json:"precision"`
		Estimate  ResourceEstimate `json:"resource_estimate"`
	}{"cpu", "float32", ResourceEstimate{RAMBytes: ram, ScratchBytes: scratch, OutputBytes: 52 * 1024 * 1024, PixelCount: pixels}})
	if err != nil {
		return nil, err
	}
	if _, err = ParseResourceEstimate(profile); err != nil {
		return nil, err
	}
	return profile, nil
}
