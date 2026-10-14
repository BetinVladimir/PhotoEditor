package v2optical

import (
 "context"
 "encoding/json"
 "os"
 "sort"
 "strings"
 "testing"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

// Pure SQL mirror parity; not evidence of scoped admission or physical authority.
func TestIntegrationMultiReplacementInventoryParity(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated migrated database required")}
 ctx,cancel:=context.WithTimeout(context.Background(),15*time.Second);defer cancel()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 ref:=func()v2input.Ref{return v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("a",64)}}
 paramsRef,calibration,report,image,pixels,alpha,field:=ref(),ref(),ref(),ref(),ref(),ref(),ref()
 task:=uuid.New()
 for _,operation:=range []string{"optical.panorama","optical.focus_stack"}{t.Run(operation,func(t *testing.T){
 entries:=[]v2input.Entry{{Ref:paramsRef,Contract:"Params"},{Ref:calibration,Contract:"Calibration"},{Ref:report,Contract:"Report"},{Ref:image,Contract:"WorkingImage"},{Ref:pixels,Contract:"NpyArray"},{Ref:alpha,Contract:"NpyArray"},{Ref:field,Contract:"NpyArray"}}
 sort.Slice(entries,func(i,j int)bool{return entries[i].Ref.ArtifactID.String()<entries[j].Ref.ArtifactID.String()})
 digest,err:=opticalDraftInputDigest(task,operation,entries);if err!=nil{t.Fatal(err)}
 source,_:=canonical.Marshal(map[string]any{"pixels":pixels,"alpha":alpha,"transform_chain":map[string]any{"steps":[]any{map[string]any{"kind":"dense_warp","inverse_field":field},map[string]any{"kind":"dense_warp","inverse_field":field}}}})
 params,_:=canonical.Marshal(map[string]any{"frames":[]any{map[string]any{"source":image},map[string]any{"source":image}}})
 packet:=map[string]any{"input_digest":digest,"draft":map[string]any{"operation":operation,"task_id":task,"params_document_id":paramsRef.ArtifactID,"params_sha":paramsRef.SHA256,"params_jcs":string(params),"sources_jcs":[]string{string(source),string(source)},"execution_profile":map[string]any{"multi_qualification":map[string]any{"calibration":calibration,"report":report}}}}
 raw,_:=canonical.Marshal(packet)
 check:=func(body []byte,want bool){t.Helper();var valid bool;if err:=db.QueryRow(ctx,"SELECT photo.v29_multi_replacement_inventory_valid($1)",string(body)).Scan(&valid);err!=nil{t.Fatal(err)};if valid!=want{t.Fatalf("inventory valid=%v want=%v",valid,want)}}
 check(raw,true)
 for _,mutate:=range []func(map[string]any){
 func(p map[string]any){p["input_digest"]=strings.Repeat("f",64)},
 func(p map[string]any){p["draft"].(map[string]any)["task_id"]=uuid.New().String()},
 func(p map[string]any){d:=p["draft"].(map[string]any);d["params_document_id"]=image.ArtifactID.String()},
 func(p map[string]any){d:=p["draft"].(map[string]any);d["sources_jcs"]=[]string{`{}`,string(source)}},
 func(p map[string]any){d:=p["draft"].(map[string]any);var s map[string]any;json.Unmarshal(source,&s);s["alpha"]=pixels;b,_:=canonical.Marshal(s);d["sources_jcs"]=[]string{string(b),string(source)}},
 }{var changed map[string]any;json.Unmarshal(raw,&changed);mutate(changed);body,_:=canonical.Marshal(changed);check(body,false)}
 })}
 for _,raw:=range []string{"",`{}`,`null`,`[]`}{var valid bool;if err=db.QueryRow(ctx,"SELECT photo.v29_multi_replacement_inventory_valid($1)",raw).Scan(&valid);err!=nil{t.Fatal(err)};if valid{t.Fatal("malformed inventory accepted")}}
}
