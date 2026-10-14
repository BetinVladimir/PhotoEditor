package delivery

import (
 "context"
 "encoding/json"
 "os"
 "testing"
 "time"
 "github.com/jackc/pgx/v5/pgxpool"
)

func TestIntegrationWatermarkPinGeometry(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated privileged database migrated through01721 required")}
 ctx,cancel:=context.WithTimeout(context.Background(),20*time.Second);defer cancel()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 scope,config,e:=watermarkPinFixture(t);pin,err:=CompileWatermarkSourcePin(scope,config,e);if err!=nil{t.Fatal(err)}
 for _,mode:=range []string{"valid_uint16_alpha","small_rgb","large_alpha","fractional_bytes","extra_array_field","oversized_canvas","alias","missing_binding","canvas_hash","extra_canvas_field","source_hash","space_id"}{t.Run(mode,func(t *testing.T){
  var body map[string]any;if err=json.Unmarshal(pin.JCS(),&body);err!=nil{t.Fatal(err)}
  arrays:=body["arrays"].([]any)
  var image map[string]any;if err=json.Unmarshal([]byte(body["manifest_jcs"].(string)),&image);err!=nil{t.Fatal(err)}
  switch mode{
  case "small_rgb":arrays[0].(map[string]any)["bytes"]=1
  case "large_alpha":arrays[1].(map[string]any)["bytes"]=100000000
  case "fractional_bytes":arrays[0].(map[string]any)["bytes"]=60000.5
  case "extra_array_field":arrays[0].(map[string]any)["ignored"]=true
  case "oversized_canvas":image["canvas"].(map[string]any)["width"]=100001
  case "alias":image["alpha"]=image["pixels"]
  case "missing_binding":delete(body,"binding")
  case "canvas_hash":body["binding"].(map[string]any)["canvas_hash"]="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
  case "extra_canvas_field":image["canvas"].(map[string]any)["ignored"]=true
  case "source_hash":image["canvas"].(map[string]any)["source_sha256"]="invalid"
  case "space_id":image["canvas"].(map[string]any)["space_id"]="invalid space"
  }
  manifest,err:=json.Marshal(image);if err!=nil{t.Fatal(err)};body["manifest_jcs"]=string(manifest)
  raw,err:=json.Marshal(body);if err!=nil{t.Fatal(err)}
  var accepted bool
  if err=db.QueryRow(ctx,`SELECT photo.v30_watermark_pin_geometry($1::jsonb)`,string(raw)).Scan(&accepted);err!=nil{t.Fatal(err)}
  if accepted!=(mode=="valid_uint16_alpha"){t.Fatalf("%s accepted=%v",mode,accepted)}
 })}
}
