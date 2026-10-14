package interchange

import (
 "context"
 "errors"
 "encoding/json"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

type importRecipePorts struct{raw []byte;old,target v2input.SessionVersionBinding;contracts []string;refs []v2input.Ref;published []byte;publishCalls int;failure string;sentinel error}
func(p *importRecipePorts)ReadImportArchiveDocument(context.Context,v2input.Ref,int64)([]byte,error){if p.failure=="read"{return nil,p.sentinel};return p.raw,nil}
func(p *importRecipePorts)QualifyImportRecipeSource(_ context.Context,_ uuid.UUID,old,target v2input.SessionVersionBinding)error{if old!=p.old||target!=p.target{return v2input.ErrInvalid};if p.failure=="source"{return p.sentinel};return nil}
func(p *importRecipePorts)RemapImportRecipeDependency(_ context.Context,_ uuid.UUID,ref v2input.Ref,contract string,old,target v2input.SessionVersionBinding)(v2input.Ref,error){
 if old!=p.old||target!=p.target{return v2input.Ref{},v2input.ErrInvalid}
 p.contracts=append(p.contracts,contract);p.refs=append(p.refs,ref)
 if p.failure=="dependency"{return v2input.Ref{},p.sentinel};if p.failure=="unmapped"{return ref,nil}
 return v2input.Ref{ArtifactID:uuid.NewSHA1(target.AssetVersionID,[]byte(ref.ArtifactID.String())),SHA256:ref.SHA256},nil
}
func(p *importRecipePorts)QualifyPreparedImportRecipe(_ context.Context,ref v2input.Ref,raw []byte,binding v2input.SessionVersionBinding)error{
 if ref.SHA256!=rawSHA(raw)||binding!=p.target{return v2input.ErrInvalid}
 if p.failure=="qualification"{return p.sentinel}
 // A validator receives its own bytes and cannot mutate the publication body.
 raw[0]='x';return nil
}
func(p *importRecipePorts)PublishPreparedImportRecipe(_ context.Context,_ uuid.UUID,ref v2input.Ref,raw []byte)(v2input.Ref,error){p.publishCalls++;p.published=append([]byte(nil),raw...);if p.failure=="receipt"{ref.SHA256=rawSHA([]byte("foreign"))};return ref,nil}

func TestPreparedImportRecipePreservesModulesOrderAndQualifiesBeforePublication(t *testing.T){
 for _,failure:=range []string{"none","read","source","dependency","unmapped","qualification","receipt","foreign_learning","hash"}{t.Run(failure,func(t *testing.T){
  ref:=func()v2input.Ref{return v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte(uuid.NewString()))}}
  old:=v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:ref(),CanvasHash:rawSHA([]byte("old canvas"))}
  target:=v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:ref(),CanvasHash:rawSHA([]byte("new canvas"))}
  overrides:=[]v2input.Ref{ref(),ref()}
  root:=map[string]any{"schema_version":3,"source":old,"base_style":ref(),"session_corrections":ref(),"learning_version_id":nil,"overrides":overrides,"layer_stack":ref(),"protected_mask":ref(),"renderer_version":"v2.2","parameter_delta":map[string]any{"ev":0.4,"log_r":0,"log_g":0,"log_b":0,"saturation_delta":0.1,"retouch_delta":0}}
  if failure=="foreign_learning"{root["learning_version_id"]=uuid.New()}
  raw,err:=canonical.Marshal(root);if err!=nil{t.Fatal(err)}
  parsed:=importParsedValidatorFixture();path,sha:="recipes/frame.json",rawSHA(raw)
  parsed.Manifest.Entries[0].RecipePath=&path;parsed.Manifest.Entries[0].RecipeSHA=&sha
  parsed.Manifest.Inventory=append(parsed.Manifest.Inventory,importBundleInventory{ID:uuid.New(),Path:path,SHA:sha,Contract:"EditingRecipeV3"})
  parsed.Files=append(parsed.Files,importParsedFile{Path:path,Content:v2input.Ref{ArtifactID:uuid.New(),SHA256:sha},Contract:"EditingRecipeV3"})
  parsedRaw,err:=canonical.Marshal(parsed);if err!=nil{t.Fatal(err)}
  ports:=&importRecipePorts{raw:raw,old:old,target:target,failure:failure,sentinel:errors.New("qualification dependency unavailable")}
  if failure=="hash"{ports.raw=[]byte(`{}`)}
  resolver,err:=NewPreparedImportRecipeResolver(uuid.New(),parsedRaw,ports,ports,ports,ports,ports);if err!=nil{t.Fatal(err)}
  receipt,err:=resolver.ResolvePreparedImportRecipe(context.Background(),parsed.Manifest.Entries[0].ID,target)
  if failure!="none"{
   if err==nil||receipt!=nil{t.Fatal("invalid recipe published as usable")}
   if failure!="receipt"&&ports.publishCalls!=0{t.Fatal("publication preceded successful qualification")}
   if failure=="read"||failure=="source"||failure=="dependency"||failure=="qualification"{if !errors.Is(err,ports.sentinel){t.Fatal("runtime failure reclassified",err)}}
   if failure=="foreign_learning"{var unsupported *ImportRecipeUnsupported;if !errors.As(err,&unsupported){t.Fatal("missing portable diagnostic",err)}}
   return
  }
  if err!=nil||receipt==nil||ports.publishCalls!=1||receipt.SHA256!=rawSHA(ports.published){t.Fatal("root publication failed",err)}
  if err:=v2input.ValidateEditingRecipeV3(ports.published,target);err!=nil{t.Fatal(err)}
  expected:=[]string{"StyleV2","CorrectionPlan","LayerStack","MaskRef","Override","Override"}
  if len(ports.contracts)!=len(expected){t.Fatal("dependency omitted")};for i,name:=range expected{if ports.contracts[i]!=name{t.Fatal("dependency contract/order changed")}}
  if ports.refs[4]!=overrides[0]||ports.refs[5]!=overrides[1]{t.Fatal("authored override order lost")}
  var prepared map[string]json.RawMessage;if err:=json.Unmarshal(ports.published,&prepared);err!=nil{t.Fatal(err)}
  delta,err:=canonical.Marshal(root["parameter_delta"]);if err!=nil||string(delta)!=string(prepared["parameter_delta"]){t.Fatal("scalar edits changed",err)}
 })}
}

func TestBoundedArchiveReaderRejectsOversizeSubstitutionAndVersionRaces(t *testing.T){
 for _,change:=range []string{"none","oversize","hash","version","mime"}{t.Run(change,func(t *testing.T){
  entry,catalog,objects,_:=importReaderFixture();catalog.object.ContentType="application/octet-stream"
  switch change{case "oversize":catalog.object.Bytes=1025;case "hash":objects.body=[]byte(`{"schema_version":2}`);case "version":catalog.change=true;case "mime":catalog.object.ContentType="application/json"}
  raw,err:=(BoundedImportArchiveReader{Catalog:catalog,Objects:objects}).ReadImportArchiveDocument(context.Background(),entry.Ref,1024)
  if change=="none"{if err!=nil||rawSHA(raw)!=entry.Ref.SHA256||catalog.calls!=2{t.Fatal("exact archive read",err)};return}
  if err==nil{t.Fatal("unqualified archive accepted")}
  if (change=="oversize"||change=="mime")&&objects.opens!=0{t.Fatal("invalid object opened")}
 })}
}
