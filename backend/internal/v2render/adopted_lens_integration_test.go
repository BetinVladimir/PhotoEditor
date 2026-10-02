package v2render

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"os"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"testing"
	"time"
)

func TestIntegrationAdoptedLensSource(t *testing.T) {
	raw := os.Getenv("PHOTO_ADOPTED_LENS_CASE")
	if raw == "" {
		t.Skip("isolated adopted lens fixture required")
	}
	var request struct {
		Tenant, Project, Snapshot, Version, Producer, Actor, Session, ResultID uuid.UUID
		ExpectedImageResult                                                    v2draft.ImageResultDocument
		Root                                                                   v2input.Ref
		WarpField                                                              v2input.Ref
		Base                                                                   v2input.SessionVersionBinding
		Bucket                                                                 string
		Object                                                                 objectstore.Object
		Revoked                                                                bool
	}
	if err := json.Unmarshal([]byte(raw), &request); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	config, err := pgxpool.ParseConfig(os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["role"] = "photo_api"
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	source := SQLAdoptedLensSource{DB: pool}
	who := auth.Identity{UserID: request.Actor, SessionID: request.Session}
	schedulerConfig := config.Copy()
	schedulerConfig.ConnConfig.RuntimeParams["role"] = "photo_scheduler"
	scheduler, err := pgxpool.NewWithConfig(ctx, schedulerConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer scheduler.Close()
	proposalCheck := SQLOriginalCompositionPreflight{DB: scheduler}
	proposalParams := LayerComposeParams{Base: request.Base, ProposalResult: request.Root}
	proposalErr := proposalCheck.CheckLensProposal(ctx, request.Tenant, request.Project, who, proposalParams)
	maskErr := proposalCheck.CheckLensMask(ctx, request.Tenant, request.Project, who, proposalParams, request.ExpectedImageResult.ChangedMask)
	if request.Revoked {
		if !errors.Is(maskErr, v2input.ErrExpired) {
			t.Fatal("revoked mask scope", maskErr)
		}
		if !errors.Is(proposalErr, v2input.ErrExpired) {
			t.Fatal("revoked lens proposal scope", proposalErr)
		}
	} else {
		if maskErr != nil {
			t.Fatal("adopted mask scope", maskErr)
		}
		wrongMask := request.ExpectedImageResult.ChangedMask
		wrongMask.ArtifactID = uuid.New()
		if err := proposalCheck.CheckLensMask(ctx, request.Tenant, request.Project, who, proposalParams, wrongMask); !errors.Is(err, v2input.ErrExpired) {
			t.Fatal("foreign mask accepted", err)
		}
		wrongMask = request.ExpectedImageResult.ChangedMask
		wrongMask.SHA256 = request.Root.SHA256
		if err := proposalCheck.CheckLensMask(ctx, request.Tenant, request.Project, who, proposalParams, wrongMask); !errors.Is(err, v2input.ErrExpired) {
			t.Fatal("wrong mask SHA accepted", err)
		}
		if err := proposalCheck.CheckLensMask(ctx, request.Tenant, request.Project, who, proposalParams, request.Root); !errors.Is(err, v2input.ErrExpired) {
			t.Fatal("ImageResult accepted as mask", err)
		}
		if proposalErr != nil {
			t.Fatal("positive lens proposal scope", proposalErr)
		}
		wrong := proposalParams
		wrong.ProposalResult.ArtifactID = uuid.New()
		if err := proposalCheck.CheckLensProposal(ctx, request.Tenant, request.Project, who, wrong); !errors.Is(err, v2input.ErrExpired) {
			t.Fatal("foreign proposal scope", err)
		}
		apiCheck := SQLOriginalCompositionPreflight{DB: pool}
		if err := apiCheck.CheckLensProposal(ctx, request.Tenant, request.Project, who, proposalParams); !errors.Is(err, v2input.ErrInvalid) {
			t.Fatal("API pool used scheduler proposal scope", err)
		}
	}
	binding, err := source.Load(ctx, who, request.Tenant, request.Project, request.Version, request.Producer, request.Root)
	if request.Revoked {
		if err == nil || binding.ResultID != uuid.Nil {
			t.Fatal("revoked session returned proof")
		}
		return
	}
	if err != nil || binding.ResultID != request.ResultID || binding.Result != request.Root || binding.Base != request.Base {
		t.Fatal("adopted binding mismatch", binding, err)
	}
	objectBinding, object, err := source.LoadObject(ctx, who, request.Tenant, request.Project, request.Version, request.Producer, request.Root, request.Bucket)
	if err != nil || objectBinding != binding || object.Key != request.Object.Key || object.VersionID != request.Object.VersionID || object.Bytes != request.Object.Bytes || object.SHA256 != request.Root.SHA256 || object.ContentType != "application/json" {
		t.Fatal("adopted object mismatch", object, err)
	}
	var storeConfig objectstore.Config
	if err := json.Unmarshal([]byte(os.Getenv("PHOTO_ADOPTED_ROOT_STORE")), &storeConfig); err != nil {
		t.Fatal("invalid isolated store config")
	}
	objects, err := objectstore.New(storeConfig)
	if err != nil {
		t.Fatal("isolated store unavailable")
	}
	readBinding, document, err := LoadAdoptedLensResult(ctx, source, objects, who, request.Tenant, request.Project, request.Version, request.Producer, request.Root, request.Bucket)
	actualSHA, _ := canonical.Hash(document)
	expectedSHA, _ := canonical.Hash(request.ExpectedImageResult)
	if err != nil || readBinding != binding || actualSHA != expectedSHA {
		t.Fatal("immutable adopted root bytes mismatch", err)
	}
	catalog, catalogBinding, err := BuildAdoptedLensCatalog(ctx, source, objects, who, request.Tenant, request.Project, request.Version, request.Producer, request.Root, request.Bucket)
	if err != nil || catalogBinding != binding {
		t.Fatal("adopted catalog construction", err)
	}
	parentRequest := CompositionPreparation{Tenant: request.Tenant, Actor: who, Base: binding.Base, ProposalResult: request.Root, Width: 5, Height: 5}
	parentErr := VerifyCompositionParents(ctx, parentRequest, catalog, objects)
	if (parentErr == nil) != (document.WorkingImage == binding.Base.Source) {
		t.Fatal("composition same-geometry parent check", parentErr)
	}
	parentRequest.Width++
	if err := VerifyCompositionParents(ctx, parentRequest, catalog, objects); err == nil {
		t.Fatal("composition dimensions not bound to source")
	}
	originalNode := v2draft.LayerRef{Ref: binding.Base.Source, Contract: "WorkingImage", Path: "TaskInputs[0]"}
	originalObject, err := catalog.Lookup(ctx, who, request.Tenant, originalNode)
	if err != nil {
		t.Fatal("original source scope", err)
	}
	originalReader, err := objects.Open(ctx, originalObject)
	if err != nil {
		t.Fatal("original source read", err)
	}
	originalBody, readErr := readAdoptedRoot(ctx, originalReader, originalObject.Bytes)
	closeErr := originalReader.Close()
	if readErr != nil || closeErr != nil {
		t.Fatal("original source integrity", readErr, closeErr)
	}
	originalImage, err := review.ParseWorkingImage(originalBody)
	if err != nil {
		t.Fatal("original source schema", err)
	}
	maskObject, err := catalog.Lookup(ctx, who, request.Tenant, v2draft.LayerRef{Ref: document.ChangedMask, Contract: "MaskRef", Path: "UserMaskSource"})
	if err != nil {
		t.Fatal("user mask source scope", err)
	}
	maskReader, err := objects.Open(ctx, maskObject)
	if err != nil {
		t.Fatal(err)
	}
	maskBody, maskReadErr := readAdoptedRoot(ctx, maskReader, maskObject.Bytes)
	maskCloseErr := maskReader.Close()
	if maskReadErr != nil || maskCloseErr != nil {
		t.Fatal("user mask pinned read", maskReadErr, maskCloseErr)
	}
	parsedMask, err := review.ParseMaskRef(maskBody)
	if err != nil {
		t.Fatal(err)
	}
	maskPixels := v2input.Ref{ArtifactID: parsedMask.Artifact.ID, SHA256: parsedMask.Artifact.SHA256}
	maskArray, err := catalog.Lookup(ctx, who, request.Tenant, v2draft.LayerRef{Ref: maskPixels, Contract: "MaskPixels", ParentRef: document.ChangedMask, Path: "UserMaskSource.artifact"})
	if err != nil {
		t.Fatal("mask binary scope", err)
	}
	arrayReader, err := objects.Open(ctx, maskArray)
	if err != nil {
		t.Fatal(err)
	}
	arrayErr := VerifyUserMaskNPY(ctx, arrayReader, maskArray.Bytes, maskPixels.SHA256, parsedMask.Canvas.Width, parsedMask.Canvas.Height)
	arrayCloseErr := arrayReader.Close()
	if arrayErr != nil || arrayCloseErr != nil {
		t.Fatal("real mask NPY validation", arrayErr, arrayCloseErr)
	}
	if document.WorkingImage == binding.Base.Source {
		if _, err := VerifyCompositionMask(ctx, who, request.Tenant, binding.Base, document.ChangedMask, catalog, objects); err != nil {
			t.Fatal("scoped composition mask byte preflight", err)
		}
		maskPublisher := v2draft.Publisher{DB: pool, Objects: objects, Bucket: request.Bucket}
		docID := uuid.NewSHA1(request.Root.ArtifactID, []byte("user-mask-fixture"))
		userMask, err := PublishUserCompositionMask(ctx, who, request.Tenant, docID, binding.Base, maskBody, originalBody, maskPublisher)
		if err != nil {
			t.Fatal("typed user mask publication", err)
		}
		replay, err := PublishUserCompositionMask(ctx, who, request.Tenant, docID, binding.Base, maskBody, originalBody, maskPublisher)
		if err != nil || replay != userMask {
			t.Fatal("typed user mask replay", err)
		}
		expr, err := canonical.Marshal(map[string]any{"op": "ref", "mask": userMask, "canvas_hash": binding.Base.CanvasHash})
		if err != nil {
			t.Fatal(err)
		}
		compiled, err := CompileComposition(Layer{ID: uuid.New(), Enabled: true, Opacity: .5, Blend: "normal", Operation: "fixture", OperationVersion: "1", Params: request.Root, Mask: expr}, binding.Base, nil, request.Root, nil, 5, 5)
		if err != nil {
			t.Fatal(err)
		}
		if err := proposalCheck.CheckUserMask(ctx, request.Tenant, request.Project, request.Snapshot, who, compiled.JSON, userMask); err != nil {
			t.Fatal("published user mask SQL scope", err)
		}
		if err := proposalCheck.CheckUserMask(ctx, request.Tenant, request.Project, request.Snapshot, who, compiled.JSON, request.Root); !errors.Is(err, v2input.ErrExpired) {
			t.Fatal("unrelated root accepted as user mask", err)
		}
		userCatalog := v2draft.SQLArtifactCatalog{DB: pool, Bucket: request.Bucket, ProjectID: request.Project, AssetVersionID: request.Version}
		values := make([]float32, 25)
		for i := range values {
			values[i] = float32(i%5) / 4
		}
		newArray := maskNPY("{'descr':'<f4','fortran_order':False,'shape':(5,5)}\n", values)
		pixelsPublisher := UserMaskPixelsPublisher{DB: pool, Objects: objects, Bucket: request.Bucket}
		newPixels, err := pixelsPublisher.Publish(ctx, who, request.Tenant, request.Version, uuid.New(), bytes.NewReader(newArray), int64(len(newArray)), maskSHA(newArray), 5, 5)
		if err != nil {
			t.Fatal("new mask for adopted proposal", err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(maskBody, &fields); err != nil {
			t.Fatal(err)
		}
		fields["artifact"], err = canonical.Marshal(newPixels)
		if err != nil {
			t.Fatal(err)
		}
		newMaskBody, err := canonical.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
		pixelsCatalog := SQLUserMaskPixelsCatalog{DB: pool, Bucket: request.Bucket}
		newMask, err := PublishVerifiedUserCompositionMask(ctx, who, request.Tenant, uuid.New(), binding.Base, newMaskBody, userCatalog, pixelsCatalog, objects, maskPublisher)
		if err != nil {
			t.Fatal("verified mask with adopted proposal", err)
		}
		newExpr, err := canonical.Marshal(map[string]any{"op": "ref", "mask": newMask, "canvas_hash": binding.Base.CanvasHash})
		if err != nil {
			t.Fatal(err)
		}
		newLayer := Layer{ID: uuid.New(), Enabled: true, Opacity: .5, Blend: "normal", Operation: "fixture", OperationVersion: "1", Params: request.Root, Mask: newExpr}
		newCompiled, err := CompileComposition(newLayer, binding.Base, nil, request.Root, nil, 5, 5)
		if err != nil {
			t.Fatal(err)
		}
		if err := proposalCheck.CheckUserMaskSet(ctx, request.Tenant, request.Project, request.Snapshot, who, newCompiled.JSON, 5, 5); err != nil {
			t.Fatal("adopted proposal bound mask set", err)
		}
		if err := VerifyCompositionParents(ctx, CompositionPreparation{Tenant: request.Tenant, Actor: who, Base: binding.Base, ProposalResult: request.Root, Width: 5, Height: 5}, catalog, objects); err != nil {
			t.Fatal("new mask proposal parents", err)
		}
		if err := VerifyCompositionMasks(ctx, who, request.Tenant, newCompiled, v2input.Ref{ArtifactID: uuid.New(), SHA256: maskSHA(newCompiled.JSON)}, catalog, objects); err != nil {
			t.Fatal("real proposal full mask bytes", err)
		}
		compositionRun := uuid.New()
		compositionWorkflow, err := v2input.WorkflowID(compositionRun)
		if err != nil {
			t.Fatal(err)
		}
		compositionTask, err := v2input.TaskID(compositionWorkflow, "layer.compose", 0)
		if err != nil {
			t.Fatal(err)
		}
		compositionRequest := CompositionPreparation{Tenant: request.Tenant, Task: compositionTask, Actor: who, Layer: newLayer, Base: binding.Base, ProposalResult: request.Root, Width: 5, Height: 5}
		compositionRequest, err = PublishCompositionParams(ctx, compositionRequest, maskPublisher)
		if err != nil {
			t.Fatal("real proposal params publication", err)
		}
		roots, err := CompositionInputRoots(newCompiled, compositionRequest.ParamsRef)
		if err != nil {
			t.Fatal(err)
		}
		resolved, err := (v2draft.ClosureResolver{Catalog: catalog, Objects: objects}).ResolveTaskInputs(ctx, who, request.Tenant, roots, binding.Base)
		if err != nil {
			t.Fatal("real proposal complete closure", err)
		}
		plan, err := CompilePinnedCompositionDraftConfig(ctx, request.Project, request.Snapshot, who.SessionID, compositionRun, []CompositionDraftRequest{{Ordinal: 0, Preparation: compositionRequest, Resolved: resolved}}, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
		if err != nil {
			t.Fatal(err)
		}
		if err := (SQLOriginalCompositionDraftStore{DB: &proposalCheck}).Begin(ctx, request.Tenant, compositionRun, who, maskSHA([]byte("synthetic-policy")), plan); err != nil {
			t.Fatal("real proposal draft", err)
		}
		if err := (SQLOriginalCompositionTaskStage{DB: scheduler}).StageLensUserMasked(ctx, request.Tenant, compositionRun, compositionWorkflow, 0, plan.Tasks[0]); err != nil {
			t.Fatal("real proposal task stage", err)
		}
		transport := artifactjson.Publisher{Repository: artifactjson.SQLRepository{DB: scheduler, Bucket: request.Bucket}, Objects: objects}
		preparedGraph, err := PrepareVerifiedComposition(ctx, compositionRequest, catalog, objects, v2input.ScopedPublisher{Documents: transport, Tenant: request.Tenant, Run: compositionRun})
		if err != nil {
			t.Fatal("real proposal verified transport", err)
		}
		if preparedGraph.GraphTask.Built.Count != len(resolved) || preparedGraph.InputDigest != plan.Tasks[0].InputDigest {
			t.Fatal("real closure digest/count mismatch")
		}
		repeatedGraph, err := PrepareVerifiedComposition(ctx, compositionRequest, catalog, objects, v2input.ScopedPublisher{Documents: transport, Tenant: request.Tenant, Run: compositionRun})
		if err != nil || repeatedGraph.GraphTask.Built.Manifest != preparedGraph.GraphTask.Built.Manifest {
			t.Fatal("real verified transport replay", err)
		}
		admission := CompositionInputAdmission{DB: scheduler}
		if err := admission.RegisterLensUserInputs(ctx, request.Tenant, compositionRun, compositionWorkflow, 0, plan.Tasks[0], preparedGraph.GraphTask); err != nil {
			t.Fatal("real composition input registration", err)
		}
		if err := admission.RegisterLensUserInputs(ctx, request.Tenant, compositionRun, compositionWorkflow, 0, plan.Tasks[0], preparedGraph.GraphTask); err != nil {
			t.Fatal("real composition input replay", err)
		}
		wrongGraph := preparedGraph.GraphTask
		wrongGraph.Built.Manifest = v2input.Ref{ArtifactID: uuid.New(), SHA256: wrongGraph.Built.Manifest.SHA256}
		if err := admission.RegisterLensUserInputs(ctx, request.Tenant, compositionRun, compositionWorkflow, 0, plan.Tasks[0], wrongGraph); !errors.Is(err, v2input.ErrInvalid) {
			t.Fatal("input replay accepted foreign manifest", err)
		}
		wrongCount := preparedGraph.GraphTask
		wrongCount.Built.Count++
		if err := admission.RegisterLensUserInputs(ctx, request.Tenant, compositionRun, compositionWorkflow, 0, plan.Tasks[0], wrongCount); !errors.Is(err, v2input.ErrInvalid) {
			t.Fatal("input registration accepted wrong count", err)
		}
		stored, err := userCatalog.Lookup(ctx, who, request.Tenant, v2draft.LayerRef{Ref: userMask, Contract: "MaskRef"})
		if err != nil {
			t.Fatal(err)
		}
		storedReader, err := objects.Open(ctx, stored)
		if err != nil {
			t.Fatal(err)
		}
		storedBody, readErr := readAdoptedRoot(ctx, storedReader, stored.Bytes)
		closeErr := storedReader.Close()
		if readErr != nil || closeErr != nil || string(storedBody) != string(maskBody) {
			t.Fatal("user mask stored bytes", readErr, closeErr)
		}
	} else if _, err := ValidateUserCompositionMask(maskBody, originalBody, binding.Base); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("different mask geometry accepted", err)
	}
	originalPixels := v2draft.LayerRef{Ref: v2input.Ref{ArtifactID: originalImage.Pixels.ID, SHA256: originalImage.Pixels.SHA256}, Contract: "WorkingPixels", ParentRef: binding.Base.Source, Path: "TaskInputs[0].pixels"}
	pixelObject, err := catalog.Lookup(ctx, who, request.Tenant, originalPixels)
	if err != nil {
		t.Fatal("original pixel scope", err)
	}
	pixelReader, err := objects.Open(ctx, pixelObject)
	if err != nil {
		t.Fatal("original pixel read", err)
	}
	pixelBytes, pixelReadErr := io.Copy(io.Discard, io.LimitReader(pixelReader, pixelObject.Bytes+1))
	pixelCloseErr := pixelReader.Close()
	if pixelReadErr != nil || pixelCloseErr != nil || pixelBytes != pixelObject.Bytes {
		t.Fatal("original pixel integrity", pixelReadErr, pixelCloseErr)
	}
	originalPixels.ParentRef = document.Report
	if _, err := catalog.Lookup(ctx, who, request.Tenant, originalPixels); err == nil {
		t.Fatal("foreign original pixel parent accepted")
	}
	if _, err := catalog.Lookup(ctx, who, request.Tenant, v2draft.LayerRef{Ref: document.ChangedMask, Contract: "MaskRef", Path: "TaskInputs[1]"}); err != nil {
		t.Fatal("independent child root", err)
	}
	if request.WarpField.ArtifactID != uuid.Nil {
		// Closure may encounter the same field through WorkingImage after
		// adoption recorded it first through changed_mask.
		field := v2draft.LayerRef{Ref: request.WarpField, Contract: "DenseWarpField", ParentRef: document.WorkingImage, Path: "TaskInputs[3].working_image.transform_chain.steps[0].inverse_field"}
		if _, err := catalog.Lookup(ctx, who, request.Tenant, field); err != nil {
			t.Fatal("shared field first-visit lookup", err)
		}
		field.Contract = "WorkingPixels"
		if _, err := catalog.Lookup(ctx, who, request.Tenant, field); err == nil {
			t.Fatal("catalog contract substitution")
		}
	}
	foreign := who
	foreign.UserID = uuid.New()
	if _, err := catalog.Lookup(ctx, foreign, request.Tenant, v2draft.LayerRef{Ref: document.Report, Contract: "ImageReport"}); err == nil {
		t.Fatal("catalog identity substitution")
	}
	{
		resolver := v2draft.ClosureResolver{Catalog: catalog, Objects: objects}
		inventory, err := resolver.ResolveTaskInputs(ctx, who, request.Tenant, []v2input.Entry{{Ref: binding.Base.Source, Contract: "WorkingImage"}, {Ref: request.Root, Contract: "ImageResult"}}, binding.Base)
		if err != nil || len(inventory) != len(catalog.entries)+2 {
			t.Fatal("real adopted task closure", len(inventory), err)
		}
	}
	items := []struct {
		ref            v2input.Ref
		path, contract string
	}{{document.Report, "$/report", "ImageReport"}, {document.ChangedMask, "$/changed_mask", "MaskRef"}}
	if request.WarpField.ArtifactID != uuid.Nil {
		fieldPath, err := adoptionChildPath("$/changed_mask", "TaskInputs[3].changed_mask", "TaskInputs[3].changed_mask.transform_chain.steps[0].inverse_field")
		if err != nil {
			t.Fatal("closure inventory path", err)
		}
		items = append(items, struct {
			ref            v2input.Ref
			path, contract string
		}{request.WarpField, fieldPath, "DenseWarpField"})
	}
	for _, item := range items {
		child, err := source.LoadChildObject(ctx, who, request.Tenant, request.Project, request.Version, request.Producer, request.Root, item.ref, item.path, item.contract, request.Bucket)
		if err != nil || child.SHA256 != item.ref.SHA256 {
			t.Fatal("adopted child lookup", err)
		}
		reader, err := objects.Open(ctx, child)
		if err != nil {
			t.Fatal("adopted child read", err)
		}
		count, readErr := io.Copy(io.Discard, io.LimitReader(reader, child.Bytes+1))
		closeErr := reader.Close()
		if readErr != nil || closeErr != nil || count != child.Bytes {
			t.Fatal("adopted child integrity", readErr, closeErr)
		}
		if _, err := source.LoadChildObject(ctx, who, request.Tenant, request.Project, request.Version, request.Producer, request.Root, item.ref, "$/wrong", item.contract, request.Bucket); err == nil {
			t.Fatal("wrong child path accepted")
		}
		if _, err := source.LoadChildObject(ctx, who, request.Tenant, request.Project, request.Version, request.Producer, request.Root, item.ref, item.path, "WorkingImage", request.Bucket); err == nil {
			t.Fatal("child contract substitution")
		}
	}
	if _, _, err := source.LoadObject(ctx, who, request.Tenant, request.Project, request.Version, request.Producer, request.Root, "wrong-bucket"); err == nil {
		t.Fatal("wrong bucket accepted")
	}
	if _, err := source.Load(ctx, who, request.Tenant, request.Project, uuid.New(), request.Producer, request.Root); err == nil {
		t.Fatal("foreign version accepted")
	}
	if _, err := source.Load(ctx, who, request.Tenant, uuid.New(), request.Version, request.Producer, request.Root); err == nil {
		t.Fatal("foreign project accepted")
	}
	wrong := request.Root
	wrong.ArtifactID = uuid.New()
	if _, err := source.Load(ctx, who, request.Tenant, request.Project, request.Version, request.Producer, wrong); err == nil {
		t.Fatal("foreign root accepted")
	}
}
