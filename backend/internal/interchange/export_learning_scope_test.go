package interchange

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)

type exportLearningAuthorityStub struct{calls int;deny bool;node v2draft.LayerRef}
func(s *exportLearningAuthorityStub)QualifyExportLearningOccurrence(ctx context.Context,node v2draft.LayerRef,raw []byte)error{
 s.calls++;s.node=node;if err:=ctx.Err();err!=nil{return err};if s.deny{return v2input.ErrInvalid};return nil
}
func TestExportFrozenLearningRequiresExactSourceOrdinalAndIndependentAuthority(t *testing.T){
 for _,contract:=range []string{"CandidateProfile","DescriptorShard"}{for _,mode:=range []string{"valid","foreign","parent","edge","authority","fallback","ordinal","hash","cancelled"}{t.Run(contract+"/"+mode,func(t *testing.T){
  ref:=func(label string)v2input.Ref{return v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte(label))}}
  _,_,workingRaw:=importWorkingValidatorFixture(t)
  image,err:=review.ParseWorkingImage(workingRaw);if err!=nil{t.Fatal(err)}
  canvasHash,err:=canonical.Hash(image.Canvas);if err!=nil{t.Fatal(err)}
  binding:=v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(workingRaw)},CanvasHash:canvasHash}
  profile:=v2draft.CandidateProfile{SchemaVersion:1,ProfileID:uuid.New(),TrainingSHA:rawSHA([]byte("training")),FeatureOrder:"scene12_v1",RidgeLambda:.1}
  for index:=range profile.Scales{profile.Scales[index]=1}
  modelRaw,err:=canonical.Marshal(profile);if err!=nil{t.Fatal(err)}
  model:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(modelRaw)}
  ordinal:=7;if mode=="ordinal"&&contract=="DescriptorShard"{ordinal=6}
  facesRaw,err:=canonical.Marshal(map[string]any{"schema_version":1,"binding":binding,"faces":[]any{}});if err!=nil{t.Fatal(err)}
  facesRef:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(facesRaw)}
  frame:=map[string]any{"schema_version":1,"binding":binding,"ordinal":ordinal,"captured_at":nil,"camera_hash":nil,"scene_embedding":make([]float64,384),"luma_quantiles":[]float64{.1,.2,.3,.4,.5},"neutral_rgb":nil,"sharpness":1,"clipping_fraction":0,"person_fraction":0,"faces":facesRef}
  descriptorRaw,err:=canonical.Marshal(map[string]any{"schema_version":1,"snapshot_digest":rawSHA([]byte("snapshot")),"frames":[]any{frame}});if err!=nil{t.Fatal(err)}
  descriptor:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(descriptorRaw)}
  version,analysis:=uuid.New(),uuid.New();style:=ref("style")
  authored,err:=canonical.Marshal(map[string]any{"schema_version":3,"source":binding,"base_style":style,"session_corrections":nil,"learning_version_id":version,"overrides":[]v2input.Ref{},"layer_stack":nil,"renderer_version":"v2.2","parameter_delta":v2draft.CorrectionVector{}});if err!=nil{t.Fatal(err)}
  body,err:=v2draft.BuildFrozenRenderRecipe(authored,v2draft.ResolvedRecipeInputs{Binding:binding,BaseStyle:style,Style:v2draft.StyleBaseline{GainsRGB:[3]float64{1,1,1},Saturation:1},LearningID:version,Learning:&v2draft.CorrectionVector{},LearningModelRef:model,LearningDescriptorRef:descriptor,LearningAnalysisID:analysis,LearningDescriptorOrdinal:7,ICCLockSHA:rawSHA([]byte("icc")),ModelLockSHA:rawSHA([]byte("models"))},nil);if err!=nil{t.Fatal(err)}
  parent:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(body)},Contract:"FrozenRenderRecipe"}
  raw:=modelRaw;entry:=v2input.Entry{Ref:model,Contract:contract}
  if contract=="DescriptorShard"{raw=descriptorRaw;entry.Ref=descriptor}
  reader:=&exportMaskNodeStub{node:ExportClosureNode{Entry:parent,Body:body,Children:[]v2input.Entry{entry}}}
  authority:=&exportLearningAuthorityStub{deny:mode=="authority"}
  fallback:=&exportSemanticsStub{deny:mode=="fallback"}
  semantics:=&ExportFrozenLearningSemantics{reader:reader,recipes:[]exportRecipeBinding{{ref:parent.Ref,binding:binding}},authority:authority,fallback:fallback}
  ctx:=context.Background()
  switch mode{case "foreign":entry.Ref.ArtifactID=uuid.New();case "parent":semantics.recipes[0].binding.AssetVersionID=uuid.New();case "edge":reader.node.Children=nil;case "hash":raw=append(raw,' ');case "cancelled":var cancel context.CancelFunc;ctx,cancel=context.WithCancel(ctx);cancel()}
  err=semantics.QualifyExportDependency(ctx,entry,raw,nil)
  valid:=mode=="valid"||(mode=="ordinal"&&contract=="CandidateProfile")
  if (err==nil)!=valid{t.Fatalf("unexpected learning scope: %v",err)}
  expectedAuthority:=0;if valid||mode=="authority"||mode=="fallback"{expectedAuthority=1}
  if authority.calls!=expectedAuthority{t.Fatal("unbound learning reached SQL authority")}
  if mode=="valid"&&contract=="DescriptorShard"{
   frameAuthority:=&exportFrameAuthorityStub{}
   sourceEntry:=v2input.Entry{Ref:binding.Source,Contract:"WorkingImage"}
   shardReader:=&exportMaskNodeStub{node:ExportClosureNode{Entry:entry,Body:descriptorRaw,Children:[]v2input.Entry{sourceEntry}}}
   workingAuthority:=ExportDescriptorWorkingAuthority{Parents:reader,Descriptors:shardReader,Recipes:semantics.recipes,Authority:frameAuthority}
   got,err:=workingAuthority.QualifyAdditionalExportWorking(ctx,sourceEntry,workingRaw)
   if err!=nil||got!=canvasHash||frameAuthority.calls!=1||frameAuthority.node.RenderFrame==nil||frameAuthority.node.RenderFrame.Ordinal!=7||frameAuthority.node.RenderFrame.Root!=parent.Ref{t.Fatal("descriptor frame context lost",err)}
   faceEntry:=v2input.Entry{Ref:facesRef,Contract:"FaceObservations"}
   shardReader.node.Children=append(shardReader.node.Children,faceEntry)
   faceValidator:=ExportDescriptorFaceValidator{Frames:workingAuthority}
   faceChildren,faceErr:=faceValidator.ExportDocumentChildren(ctx,faceEntry,facesRaw)
   if faceErr!=nil||len(faceChildren)!=1||faceChildren[0]!=sourceEntry||frameAuthority.node.Contract!="FaceObservations"||frameAuthority.node.ParentRef!=entry.Ref{t.Fatal("descriptor faces context lost",faceErr)}
   pixelsEntry:=v2input.Entry{Ref:v2input.Ref{ArtifactID:image.Pixels.ID,SHA256:image.Pixels.SHA256},Contract:"WorkingPixels"}
   if err:=faceValidator.QualifyExportWorkingArrayOccurrence(ctx,sourceEntry,workingRaw,pixelsEntry);err!=nil{t.Fatal("descriptor pixels refused",err)}
   if frameAuthority.arrayCalls!=1||frameAuthority.arrayNode.RenderFrame==nil||frameAuthority.arrayNode.RenderFrame.Ordinal!=7||frameAuthority.arrayNode.ParentRef!=binding.Source||frameAuthority.arrayNode.ExpectedBinding==nil{t.Fatal("descriptor pixels context lost")}
   frameAuthority.arrayDeny=true
   if err:=faceValidator.QualifyExportWorkingArrayOccurrence(ctx,sourceEntry,workingRaw,pixelsEntry);err==nil{t.Fatal("revoked descriptor array accepted")}
   frameAuthority.arrayDeny=false
   frameAuthority.deny=true
   if _,err:=workingAuthority.QualifyAdditionalExportWorking(ctx,sourceEntry,workingRaw);err==nil{t.Fatal("revoked frame accepted")}
   if _,err:=faceValidator.ExportDocumentChildren(ctx,faceEntry,facesRaw);err==nil{t.Fatal("revoked faces accepted")}
   shardReader.node.Children=nil
   if _,err:=workingAuthority.QualifyAdditionalExportWorking(ctx,sourceEntry,workingRaw);err==nil{t.Fatal("missing descriptor frame edge accepted")}
  }
  if valid&&(authority.node.ExpectedLearning==nil||authority.node.ExpectedLearning.AnalysisID!=analysis||authority.node.ExpectedLearning.VersionID!=version||authority.node.ParentRef!=parent.Ref){t.Fatal("learning pins lost")}
 })}}
}


type exportFrameAuthorityStub struct{arrayCalls int;arrayNode v2draft.LayerRef;arrayDeny bool;calls int;node v2draft.LayerRef;deny bool}
func(s *exportFrameAuthorityStub)QualifyExportRenderFrame(ctx context.Context,node v2draft.LayerRef,raw []byte)error{
 s.calls++;s.node=node;if s.deny{return v2input.ErrInvalid};return ctx.Err()
}
func TestSQLExportRenderFrameRejectsMissingContext(t *testing.T){
 if err:=(SQLExportDocumentCatalog{}).QualifyExportRenderFrame(context.Background(),v2draft.LayerRef{},nil);err!=v2input.ErrInvalid{t.Fatal(err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if err:=(SQLExportDocumentCatalog{}).QualifyExportRenderFrame(ctx,v2draft.LayerRef{},nil);err!=context.Canceled{t.Fatal(err)}
}

func(s *exportFrameAuthorityStub)QualifyExportRenderFrameArray(ctx context.Context,node v2draft.LayerRef)error{
 s.arrayCalls++;s.arrayNode=node;if s.arrayDeny{return v2input.ErrInvalid};return ctx.Err()
}
func TestExportDescriptorArrayRejectsAbsentContextAndInstallation(t *testing.T){
 ctx:=context.Background()
 if err:=(SQLExportDocumentCatalog{}).QualifyExportRenderFrameArray(ctx,v2draft.LayerRef{});err!=v2input.ErrInvalid{t.Fatal(err)}
 if err:=(ExportJSONNodeResolver{}).QualifyExportWorkingArrayOccurrence(ctx,v2input.Entry{},nil,v2input.Entry{});err!=v2input.ErrInvalid{t.Fatal(err)}
 ctx,cancel:=context.WithCancel(ctx);cancel()
 if err:=(SQLExportDocumentCatalog{}).QualifyExportRenderFrameArray(ctx,v2draft.LayerRef{});err!=context.Canceled{t.Fatal(err)}
}
