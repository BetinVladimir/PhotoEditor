// Fixture-only public compilers: no database, physical execution or qualification.
package main

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "fmt"
 "io"
 "os"
 "strings"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2render"
)

type fixtureGeometryCatalog struct {calibration v2render.RenderCalibration}
func (f fixtureGeometryCatalog) ResolveAuthoredGeometry(_ context.Context,q v2render.RenderQualification)(v2render.RenderCalibration,error){if q!=f.calibration.Qualification{return v2render.RenderCalibration{},fmt.Errorf("fixture qualification mismatch")};return f.calibration,nil}

func main() {
 if len(os.Args)!=2 { panic("fixture kind required") }
 d:=json.NewDecoder(os.Stdin);d.DisallowUnknownFields()
 var raw []byte;var err error
 switch os.Args[1] {
 case "segment":
  var p v2render.AuthoredSegmentParams
  if err=d.Decode(&p);err==nil {var c v2render.CompiledAuthoredSegmentParams;c,err=v2render.CompileAuthoredSegmentParams(p);raw=c.JSON}
 case "geometry":
  var p v2render.AuthoredGeometryTaskParams
  if err=d.Decode(&p);err==nil {var c v2render.CompiledAuthoredGeometryTaskParams;c,err=v2render.CompileAuthoredGeometryTaskParams(p);raw=c.JSON}
 case "final":
  var p v2render.AuthoredFinalParams
  if err=d.Decode(&p);err==nil {var c v2render.CompiledAuthoredFinalParams;c,err=v2render.CompileAuthoredFinalParams(p);raw=c.JSON}
 case "recipe":
  var p struct {Binding v2input.SessionVersionBinding `json:"binding"`;LayerJCS string `json:"layer_jcs"`;Layer v2input.Ref `json:"layer"`;Style v2input.Ref `json:"style"`}
  if err=d.Decode(&p);err==nil {
   h:=sha256.Sum256([]byte(p.LayerJCS));layer:=p.Layer
   if layer.ArtifactID==uuid.Nil || layer.SHA256!=hex.EncodeToString(h[:]) {err=fmt.Errorf("invalid fixture layer");break}
   var authored []byte
   authored,err=canonical.Marshal(map[string]any{"schema_version":3,"source":p.Binding,"base_style":p.Style,"session_corrections":nil,"learning_version_id":nil,"overrides":[]v2input.Ref{},"layer_stack":layer,"renderer_version":"v2.2","parameter_delta":v2draft.CorrectionVector{}})
   if err==nil {raw,err=v2draft.BuildFrozenRenderRecipe(authored,v2draft.ResolvedRecipeInputs{Binding:p.Binding,BaseStyle:p.Style,Style:v2draft.StyleBaseline{GainsRGB:[3]float64{1,1,1},Saturation:1},ICCLockSHA:strings.Repeat("a",64),ModelLockSHA:strings.Repeat("a",64)},[]byte(p.LayerJCS))}
  }
 case "geometry-profile":
  var p struct {SourceJCS string `json:"source_jcs"`;Calibration v2render.RenderCalibration `json:"calibration"`}
  if err=d.Decode(&p);err==nil {var source review.WorkingImage;source,err=review.ParseWorkingImage([]byte(p.SourceJCS));if err==nil{raw,err=v2render.CompileAuthoredGeometryExecutionProfile(context.Background(),fixtureGeometryCatalog{p.Calibration},p.Calibration.Qualification,int64(source.Canvas.Width),int64(source.Canvas.Height),source.Color.ICCSHA256)}}
 case "final-profile", "segment-profile":
  var p struct {Tenant uuid.UUID `json:"tenant"`;Binding v2input.SessionVersionBinding `json:"binding"`;Recipe v2input.Ref `json:"recipe"`;RecipeJCS string `json:"recipe_jcs"`;SourceJCS string `json:"source_jcs"`;TargetJCS string `json:"target_jcs"`;Params json.RawMessage `json:"params"`;LayerJCS string `json:"layer_jcs"`;Calibration v2render.RenderCalibration `json:"calibration"`}
  if err=d.Decode(&p);err==nil {
   r:=v2render.RenderDraftRequest{Tenant:p.Tenant,Binding:p.Binding,RecipeRef:p.Recipe,RecipeRaw:[]byte(p.RecipeJCS),SourceRaw:[]byte(p.SourceJCS),LayerRaw:[]byte(p.LayerJCS)}
   if os.Args[1]=="final-profile" {
    var params v2render.AuthoredFinalParams;pd:=json.NewDecoder(strings.NewReader(string(p.Params)));pd.DisallowUnknownFields();err=pd.Decode(&params)
    if err==nil {var c v2render.CompiledAuthoredFinalParams;c,err=v2render.CompileAuthoredFinalParams(params);if err==nil{raw,err=v2render.CompileAuthoredFinalExecutionProfile(r,c,[]byte(p.TargetJCS),p.Calibration.Qualification,p.Calibration)}}
   } else {
    var params v2render.AuthoredSegmentParams;pd:=json.NewDecoder(strings.NewReader(string(p.Params)));pd.DisallowUnknownFields();err=pd.Decode(&params)
    if err==nil {var c v2render.CompiledAuthoredSegmentParams;c,err=v2render.CompileAuthoredSegmentParams(params);if err==nil{raw,err=v2render.CompileAuthoredSegmentExecutionProfile(r,c,[]byte(p.TargetJCS),nil,p.Calibration.Qualification,p.Calibration)}}
   }
  }
 default:panic("unsupported fixture kind")
 }
 if err==nil {var extra any;if d.Decode(&extra)!=io.EOF {err=fmt.Errorf("trailing fixture data")}}
 if err!=nil {fmt.Fprintln(os.Stderr,"COMPILER_FIXTURE_REJECTED");os.Exit(1)}
 _,_ = os.Stdout.Write(raw)
}
