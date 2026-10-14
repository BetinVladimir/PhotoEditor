package workflow

import (
 "bytes"
 "context"
 "encoding/json"
 "os"
 "testing"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
)

type derivativeParityCase struct{name,operation string;w,h int64;alpha,upscale bool;long,width,height int64;pixels int64}
func derivativeParityCases()[]derivativeParityCase{return []derivativeParityCase{
 {name:"tie down",operation:"derivative.resize",w:10,h:5,long:5,pixels:10},
 {name:"tie up alpha",operation:"derivative.resize",w:10,h:7,long:5,alpha:true,pixels:20},
 {name:"contain",operation:"derivative.resize",w:13,h:7,width:4,height:4,pixels:8},
 {name:"upscale disabled",operation:"derivative.resize",w:4,h:3,long:8,pixels:12},
 {name:"upscale alpha",operation:"derivative.resize",w:4,h:3,long:8,alpha:true,upscale:true,pixels:48},
 {name:"minimum one",operation:"derivative.resize",w:100,h:1,long:1,pixels:1},
 {name:"sharpen",operation:"derivative.sharpen",w:13,h:7,pixels:91},
 {name:"sharpen alpha",operation:"derivative.sharpen",w:13,h:7,alpha:true,pixels:91},
}}
func derivativeParityFixture(t *testing.T,c derivativeParityCase)(CompiledDownloadNode,[]byte){
 t.Helper();_,_,_,raw,_:=deliveryExportPlanFixture(t,nil)
 image,err:=review.ParseWorkingImage(raw);if err!=nil{t.Fatal(err)}
 image.Canvas.Width=c.w;image.Canvas.Height=c.h;image.Alpha=nil
 if c.alpha{alpha:=review.ArtifactRef{ID:uuid.New(),SHA256:downloadDigest("profile alpha")};image.Alpha=&alpha}
 raw,err=canonical.Marshal(image);if err!=nil{t.Fatal(err)}
 ref:=review.ArtifactRef{ID:uuid.New(),SHA256:renderDerivativeBytesSHA(raw)}
 inputs:=[]review.ArtifactRef{ref,image.Pixels};if image.Alpha!=nil{inputs=append(inputs,*image.Alpha)}
 params:=map[string]any{"image":ref}
 if c.operation=="derivative.resize"{params["fit"]="contain";params["allow_upscale"]=c.upscale;if c.long>0{params["long_edge"]=c.long}else{params["width"]=c.width;params["height"]=c.height}}
 body,err:=canonical.Marshal(params);if err!=nil{t.Fatal(err)}
 return CompiledDownloadNode{Version:"1",ResourceClass:"cpu",Operation:c.operation,Inputs:inputs,Params:body},raw
}
func TestDeliveryDerivativeProfileRoundingAndChannels(t *testing.T){
 for _,c:=range derivativeParityCases(){t.Run(c.name,func(t *testing.T){
  node,raw:=derivativeParityFixture(t,c)
  profile,err:=CompileDeliveryDerivativeProfile(node,raw,DeliveryDerivativeLimits{RAMBytes:1<<40,ScratchBytes:1<<40,OutputBytes:1<<40});if err!=nil{t.Fatal(err)}
  var got struct{Resource ResourceEstimate `json:"resource_estimate"`};if err=json.Unmarshal(profile,&got);err!=nil{t.Fatal(err)}
  channels:=int64(12);if c.alpha{channels=16}
  want:=ResourceEstimate{RAMBytes:(256<<20)+192*(c.w*c.h+c.pixels),VRAMBytes:0,ScratchBytes:(32<<20)+channels*(c.w*c.h+c.pixels),OutputBytes:(8<<20)+channels*c.pixels,PixelCount:c.pixels}
  if got.Resource!=want{t.Fatal(got.Resource,want)}
 })}
}
func TestIntegrationWatermarkDerivativeSQLGoProfileParity(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated privileged database migrated through01779 required")}
 ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second);defer cancel()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 for _,c:=range derivativeParityCases(){t.Run(c.name,func(t *testing.T){
  node,raw:=derivativeParityFixture(t,c)
  expected,err:=CompileDeliveryDerivativeProfile(node,raw,DeliveryDerivativeLimits{RAMBytes:1<<40,ScratchBytes:1<<40,OutputBytes:1<<40});if err!=nil{t.Fatal(err)}
  tx,err:=db.Begin(ctx);if err!=nil{t.Fatal(err)};defer tx.Rollback(ctx)
  if _,err=tx.Exec(ctx,"SET LOCAL ROLE photo_fn_owner");err!=nil{t.Fatal(err)}
  var actual []byte
  if err=tx.QueryRow(ctx,`SELECT photo.v30_watermark_derivative_profile($1,$2,$3)`,node.Operation,string(node.Params),string(raw)).Scan(&actual);err!=nil{t.Fatal(err)}
  actual,err=canonical.Bytes(actual);if err!=nil{t.Fatal(err)}
  if !bytes.Equal(actual,expected){t.Fatalf("SQL/Go profile mismatch: SQL=%s Go=%s",actual,expected)}
 })}
}

func malformedDerivativeResizeParams(t *testing.T,node CompiledDownloadNode,mode string)[]byte{
 t.Helper();var params map[string]any
 if err:=json.Unmarshal(node.Params,&params);err!=nil{t.Fatal(err)}
 switch mode{
 case "null edge":params["long_edge"]=nil
 case "missing upscale":delete(params,"allow_upscale")
 case "null upscale":params["allow_upscale"]=nil
 case "string edge":params["long_edge"]="5"
 case "fractional edge":params["long_edge"]=2.5
 case "zero edge":params["long_edge"]=0
 case "oversized edge":params["long_edge"]=100001
 case "null box":delete(params,"long_edge");params["width"]=nil;params["height"]=5
 case "missing height":delete(params,"long_edge");params["width"]=5
 case "mixed sizing":params["width"]=5;params["height"]=5
 default:t.Fatal("unknown malformed case",mode)
 }
 body,err:=canonical.Marshal(params);if err!=nil{t.Fatal(err)};return body
}
var malformedDerivativeResizeModes=[]string{"null edge","missing upscale","null upscale","string edge","fractional edge","zero edge","oversized edge","null box","missing height","mixed sizing"}
func TestDerivativeProfileRejectsMissingOrNullResizeFields(t *testing.T){
 node,raw:=derivativeParityFixture(t,derivativeParityCases()[0])
 for _,mode:=range malformedDerivativeResizeModes{t.Run(mode,func(t *testing.T){
  changed:=node;changed.Params=malformedDerivativeResizeParams(t,node,mode)
  if _,err:=CompileDeliveryDerivativeProfile(changed,raw,DeliveryDerivativeLimits{RAMBytes:1<<40,ScratchBytes:1<<40,OutputBytes:1<<40});err==nil{t.Fatal("malformed resize profile accepted")}
 })}
}
func TestIntegrationWatermarkDerivativeSQLRejectsMalformedResize(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated privileged database migrated through01779 required")}
 ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second);defer cancel()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 node,raw:=derivativeParityFixture(t,derivativeParityCases()[0])
 for _,mode:=range malformedDerivativeResizeModes{t.Run(mode,func(t *testing.T){
  params:=malformedDerivativeResizeParams(t,node,mode)
  tx,err:=db.Begin(ctx);if err!=nil{t.Fatal(err)};defer tx.Rollback(ctx)
  if _,err=tx.Exec(ctx,"SET LOCAL ROLE photo_fn_owner");err!=nil{t.Fatal(err)}
  var refused bool
  if err=tx.QueryRow(ctx,`SELECT photo.v30_watermark_derivative_profile($1,$2,$3) IS NULL`,node.Operation,string(params),string(raw)).Scan(&refused);err!=nil||!refused{t.Fatal("SQL accepted malformed resize",mode,refused,err)}
 })}
}

func malformedDerivativeSourceScalars()[][3]string{return [][3]string{
 {"string_canvas_width",`"width":10`,`"width":"10"`},
 {"wrong_color_encoding",`"encoding":"gamma24"`,`"encoding":"linear"`},
 {"wrong_orientation",`"orientation":1`,`"orientation":2`},
 {"invalid_icc_digest","",""},
 {"invalid_source_digest","",""},
 {"invalid_space_id","",""},
}}
func derivativeSourceScalarFixture(t *testing.T,mode [3]string)(CompiledDownloadNode,[]byte){
 t.Helper();node,raw:=derivativeParityFixture(t,derivativeParityCases()[0])
 var changed []byte
 if mode[1]!=""{
  changed=bytes.Replace(raw,[]byte(mode[1]),[]byte(mode[2]),1)
 }else{
  image,err:=review.ParseWorkingImage(raw);if err!=nil{t.Fatal(err)}
  switch mode[0]{
  case "invalid_icc_digest":image.Color.ICCSHA256="bad"
  case "invalid_source_digest":image.Canvas.SourceSHA256="bad"
  case "invalid_space_id":image.Canvas.SpaceID="bad space"
  default:t.Fatal("unknown source scalar",mode[0])
  }
  changed,err=canonical.Marshal(image);if err!=nil{t.Fatal(err)}
 }
 if bytes.Equal(changed,raw){t.Fatal("source scalar fixture missing",mode[0])}
 node.Inputs[0].SHA256=renderDerivativeBytesSHA(changed)
 var params map[string]any
 if err:=json.Unmarshal(node.Params,&params);err!=nil{t.Fatal(err)}
 params["image"]=node.Inputs[0]
 var err error
 node.Params,err=canonical.Marshal(params);if err!=nil{t.Fatal(err)}
 return node,changed
}
func TestWatermarkDerivativeProfileRejectsInvalidSourceScalars(t *testing.T){
 for _,mode:=range malformedDerivativeSourceScalars(){t.Run(mode[0],func(t *testing.T){
  node,raw:=derivativeSourceScalarFixture(t,mode)
  if _,err:=CompileDeliveryDerivativeProfile(node,raw,DeliveryDerivativeLimits{RAMBytes:1<<40,ScratchBytes:1<<40,OutputBytes:1<<40});err==nil{t.Fatal("Go accepted invalid source scalar")}
 })}
}
func TestIntegrationWatermarkDerivativeSQLRejectsInvalidSourceScalars(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated privileged database migrated through01779 required")}
 ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second);defer cancel()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 for _,mode:=range malformedDerivativeSourceScalars(){t.Run(mode[0],func(t *testing.T){
  node,raw:=derivativeSourceScalarFixture(t,mode)
  tx,err:=db.Begin(ctx);if err!=nil{t.Fatal(err)};defer tx.Rollback(ctx)
  if _,err=tx.Exec(ctx,"SET LOCAL ROLE photo_fn_owner");err!=nil{t.Fatal(err)}
  var refused bool
  if err=tx.QueryRow(ctx,`SELECT photo.v30_watermark_derivative_profile($1,$2,$3) IS NULL`,node.Operation,string(node.Params),string(raw)).Scan(&refused);err!=nil||!refused{t.Fatal("SQL accepted invalid source scalar",mode[0],refused,err)}
 })}
}
