package v2learning

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type caseDocuments struct{ value TrainingEditingDocuments }

func (s caseDocuments) Load(context.Context, TrainingStart,
	TrainingCandidate) (TrainingEditingDocuments, error) {
	return s.value, nil
}

type caseRecipes struct{ value CompiledTrainingRecipes }

func (s caseRecipes) LoadPair(context.Context, auth.Identity, uuid.UUID,
	TrainingEditingDocuments, string, string) (CompiledTrainingRecipes,
	*v2draft.ResolvedCorrection, error) {
	return s.value, nil, nil
}

type caseImage struct{ value TrainingImage }

func (s caseImage) Load(context.Context, auth.Identity, uuid.UUID,
	v2input.SessionVersionBinding, string) (TrainingImage, error) {
	return s.value, nil
}
func (s caseImage) DiscoverICC(context.Context, auth.Identity, uuid.UUID,
	v2input.SessionVersionBinding) (string, error) {
	return s.value.Document.Color.ICCSHA256, nil
}

type caseDescriptor struct{ value TrainingDescriptor }

func (s caseDescriptor) Load(context.Context, auth.Identity, uuid.UUID,
	uuid.UUID, v2input.SessionVersionBinding, string,
	*v2draft.ResolvedCorrection) (TrainingDescriptor, error) {
	return s.value, nil
}

type caseFaces struct{ calls *int }

func (s caseFaces) Load(context.Context, auth.Identity, uuid.UUID,
	v2input.SessionVersionBinding, v2input.Ref) (int, error) {
	*s.calls++
	return 0, nil
}

type caseProtection struct{ ref v2input.Ref }

func (s caseProtection) SelectProtected(context.Context, auth.Identity,
	TrainingStart, TrainingCandidate, TrainingImage) (v2input.Ref,
	TrainingMask, error) {
	return s.ref, TrainingMask{}, nil
}

type cancellingCaseDocuments struct {
 TrainingDocumentReader
 cancel context.CancelFunc
}
func (s cancellingCaseDocuments) Load(ctx context.Context, start TrainingStart, candidate TrainingCandidate) (TrainingEditingDocuments,error) {
 value,err := s.TrainingDocumentReader.Load(ctx,start,candidate)
 s.cancel()
 return value,err
}
type cancellingCaseProtection struct {
 TrainingProtectedSelector
 cancel context.CancelFunc
}
func (s cancellingCaseProtection) SelectProtected(ctx context.Context, who auth.Identity, start TrainingStart, candidate TrainingCandidate, image TrainingImage) (v2input.Ref,TrainingMask,error) {
 ref,mask,err := s.TrainingProtectedSelector.SelectProtected(ctx,who,start,candidate,image)
 s.cancel()
 return ref,mask,err
}

type mutatingCaseRecipes struct { TrainingRecipeReader }
func (s mutatingCaseRecipes) LoadPair(ctx context.Context, who auth.Identity, tenant uuid.UUID, documents TrainingEditingDocuments, icc,model string) (CompiledTrainingRecipes,*v2draft.ResolvedCorrection,error) {
 result,session,err:=s.TrainingRecipeReader.LoadPair(ctx,who,tenant,documents,icc,model)
 if len(documents.ApprovedBody)>0 { documents.ApprovedBody[0]='!' }
 return result,session,err
}
type mutatingCaseImage struct { TrainingImageReader; mutate func() }
func (s mutatingCaseImage) Load(ctx context.Context,who auth.Identity,tenant uuid.UUID,binding v2input.SessionVersionBinding,icc string) (TrainingImage,error) {
 s.mutate()
 return s.TrainingImageReader.Load(ctx,who,tenant,binding,icc)
}

type caseRegionReader struct { load func(context.Context,v2input.Ref)([]TrainingRegionMask,error) }
func (r caseRegionReader) LoadRegions(ctx context.Context,who auth.Identity,start TrainingStart,candidate TrainingCandidate,image TrainingImage,style v2input.Ref)([]TrainingRegionMask,error){return r.load(ctx,style)}

func TestTrainingCaseAssemblerBindsSourceLineageAndPinnedInputs(t *testing.T) {
	ref := func(letter string) v2input.Ref {
		return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat(letter, 64)}
	}
	working, style := ref("a"), ref("b")
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(),
		Source: working, CanvasHash: strings.Repeat("c", 64)}
	icc, model, lineage := strings.Repeat("d", 64), strings.Repeat("e", 64), strings.Repeat("f", 64)
	base := FrozenRecipe{SchemaVersion: 1, Source: binding,
		BaseStyle: style, Renderer: "v2.2", ICCLockSHA: icc,
		ModelLockSHA: model}
	approved := base
	approved.EffectiveDelta.EV = .25
	encode := func(recipe FrozenRecipe) ([]byte, string) {
		body, err := canonical.Marshal(recipe)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(body)
		return body, hex.EncodeToString(sum[:])
	}
	baseBody, baseSHA := encode(base)
	approvedBody, approvedSHA := encode(approved)
	compiled := CompiledTrainingRecipes{Base: base, Approved: approved,
		BaseBody: baseBody, BaseSHA: baseSHA,
		ApprovedBody: approvedBody, ApprovedSHA: approvedSHA}
	baseDoc, approvedDoc := ref("1"), ref("2")
	approval := uuid.New()
	candidate := TrainingCandidate{EventID: uuid.New(), SessionID: uuid.New(),
		AssetVersionID:   binding.AssetVersionID,
		SourceContentSHA: working.SHA256, SourceLineageSHA: lineage,
		BaseDocumentID: uuid.New(), BaseArtifactID: baseDoc.ArtifactID,
		BaseSHA: baseDoc.SHA256, ApprovedDocumentID: uuid.New(),
		ApprovedArtifactID: approvedDoc.ArtifactID,
		ApprovedSHA:        approvedDoc.SHA256, ApprovalID: &approval,
		ApprovedAt: time.Now().UTC(), ConsentID: uuid.New(), ConsentEpoch: 1,
		ChangedPaths: []string{"/ev"}, ConsentLive: true,
		SourceReady: true, BaseReady: true, ApprovedReady: true}
	documents := TrainingEditingDocuments{BaseDocumentID: candidate.BaseDocumentID,
		ApprovedDocumentID: candidate.ApprovedDocumentID,
		Binding:            binding, BaseStyle: style,
		ApprovedBody: []byte(`{}`)}
	facesCalls := 0
	assembler := TrainingCaseAssembler{Documents: caseDocuments{documents},
		Recipes: caseRecipes{compiled},
		Images: caseImage{TrainingImage{Document: review.WorkingImage{
			Canvas: review.Canvas{SourceSHA256: lineage},
			Color:  review.ColorSpace{ICCSHA256: icc}}, PixelsCount: 1}},
		Descriptors: caseDescriptor{TrainingDescriptor{Faces: ref("3"),
			Shard: ref("4"), AnalysisID: uuid.New()}},
		Faces:      caseFaces{&facesCalls},
		Protection: caseProtection{ref("5")}}
	start := TrainingStart{TenantID: uuid.New(), ProjectID: uuid.New(),
		ActorID: uuid.New(), AuthSessionID: uuid.New()}
	if discovered, err := assembler.DiscoverICC(context.Background(), start,
		candidate); err != nil || discovered != icc {
		t.Fatalf("case ICC discovery: %q, %v", discovered, err)
	}
	runID := uuid.New()
	got, err := assembler.Assemble(context.Background(), start, candidate,
		runID, icc, model)
	if err != nil || got.Source.Target[0] != .25 || facesCalls != 1 ||
		got.Resolved.Render.Protected.ArtifactID == uuid.Nil ||
		got.Resolved.Render.DescriptorShard.ArtifactID == uuid.Nil {
		t.Fatalf("case not assembled: %v", err)
	}
 t.Run("regions_frozen_by_assembler",func(t *testing.T) {
  retained:=[]TrainingRegionMask{{RegionID:"sky",Mask:ref("6")}}
  expected:=retained[0]
  owned:=assembler
  owned.Regions=caseRegionReader{load:func(ctx context.Context,pinned v2input.Ref)([]TrainingRegionMask,error){
   if pinned!=style{t.Fatal("region reader received different style")}
   return retained,nil
  }}
  result,err:=owned.Assemble(context.Background(),start,candidate,runID,icc,model)
  if err!=nil||len(result.Resolved.Render.RegionMasks)!=1||result.Resolved.Render.RegionMasks[0]!=expected{t.Fatalf("region evidence lost: %v",err)}
  retained[0].RegionID="mutated"
  if result.Resolved.Render.RegionMasks[0]!=expected{t.Fatal("retained reader changed frozen region")}
 })
 t.Run("regions_cancellation_refuses_frozen_case",func(t *testing.T) {
  ctx,cancel:=context.WithCancel(context.Background());defer cancel()
  owned:=assembler
  owned.Regions=caseRegionReader{load:func(context.Context,v2input.Ref)([]TrainingRegionMask,error){cancel();return []TrainingRegionMask{{RegionID:"sky",Mask:ref("6")}},nil}}
  result,err:=owned.Assemble(ctx,start,candidate,runID,icc,model)
  if !errors.Is(err,context.Canceled)||result.Frozen!=nil{t.Fatalf("cancelled region evidence escaped: %v",err)}
 })
 t.Run("reader_documents_owned",func(t *testing.T) {
  owned:=assembler
  owned.Recipes=mutatingCaseRecipes{assembler.Recipes}
  if _,err:=owned.Assemble(context.Background(),start,candidate,runID,icc,model); err!=nil { t.Fatal(err) }
  if string(documents.ApprovedBody)!="{}" { t.Fatal("reader mutated retained source document") }
 })
 t.Run("compiled_bytes_owned_before_image_callback",func(t *testing.T) {
  retained:=ownTrainingAssemblyRecipes(compiled)
  owned:=assembler
  owned.Recipes=caseRecipes{retained}
  owned.Images=mutatingCaseImage{assembler.Images,func(){ retained.BaseBody[0]='!'; retained.ApprovedBody[0]='!' }}
  result,err:=owned.Assemble(context.Background(),start,candidate,runID,icc,model)
  if err!=nil || result.Frozen==nil || result.Frozen.BaseBody[0]!='{' { t.Fatalf("retained callback changed compiled bytes: %v",err) }
 })
 for _,phase := range []string{"before","documents","protection"} {
  t.Run("cancel_"+phase,func(t *testing.T) {
   ctx,cancel := context.WithCancel(context.Background())
   defer cancel()
   owned := assembler
   if phase=="before" { cancel() }
   if phase=="documents" { owned.Documents=cancellingCaseDocuments{assembler.Documents,cancel} }
   if phase=="protection" { owned.Protection=cancellingCaseProtection{assembler.Protection,cancel} }
   beforeCalls:=facesCalls
   result,err:=owned.Assemble(ctx,start,candidate,runID,icc,model)
   if !errors.Is(err,context.Canceled) || result.Frozen!=nil {
    t.Fatalf("cancelled assembly returned evidence: %v",err)
   }
   if phase!="protection" && facesCalls!=beforeCalls { t.Fatal("cancelled assembly reached face reader") }
  })
 }
 ctx,cancel:=context.WithCancel(context.Background())
 discovery:=assembler
 discovery.Documents=cancellingCaseDocuments{assembler.Documents,cancel}
 if lock,err:=discovery.DiscoverICC(ctx,start,candidate); !errors.Is(err,context.Canceled) || lock!="" {
  t.Fatalf("cancelled discovery returned ICC: %q %v",lock,err)
 }
 cancel()
	candidate.SourceLineageSHA = strings.Repeat("0", 64)
	if _, err := assembler.Assemble(context.Background(), start, candidate,
		runID, icc, model); err == nil {
		t.Fatal("foreign source lineage accepted")
	}
}
