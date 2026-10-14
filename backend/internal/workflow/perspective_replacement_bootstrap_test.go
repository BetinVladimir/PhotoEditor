package workflow

import (
 "context"
 "errors"
 "crypto/sha256"
 "encoding/hex"
 "strings"
 "testing"

)

func TestPerspectiveReplacementBootstrapOptionalAndDependencies(t *testing.T){
 t.Setenv("PHOTO_V29_PERSPECTIVE_CONFIG","")
 projects,err:=buildPerspectiveReplacementProjects(context.Background(),nil,nil,"")
 if err!=nil||projects!=nil{t.Fatal("optional configuration required",err)}
 t.Setenv("PHOTO_V29_PERSPECTIVE_CONFIG","/not-read-without-dependencies")
 if _,err=buildPerspectiveReplacementProjects(context.Background(),nil,nil,"photos");err==nil{t.Fatal("missing dependencies accepted")}
 t.Setenv("PHOTO_V29_NEGATIVE_CONFIG","")
 if _,err=buildNegativeReplacementScheduler(context.Background(),nil,nil,"photos");err==nil{t.Fatal("orphan perspective deployment silently ignored")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err=buildPerspectiveReplacementProjects(ctx,nil,nil,"");!errors.Is(err,context.Canceled){t.Fatal(err)}
}

// These refusal tests do not establish deployment or database qualification.
func TestPerspectiveSchedulerDeploymentRefusesUntrustedConfiguration(t *testing.T) {
 digest := func(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }
 cases := []struct{name string; raw []byte; pin string}{
  {"empty", nil, strings.Repeat("0",64)},
  {"oversized", []byte(strings.Repeat("x",(8<<20)+1)), strings.Repeat("0",64)},
  {"missing pin", []byte(`{}`), ""},
  {"malformed pin", []byte(`{}`), "invalid"},
  {"wrong bytes", []byte(`{}`), digest([]byte(`[]`))},
  {"invalid json", []byte(`xx`), digest([]byte(`xx`))},
  {"empty project inventory", []byte(`{"schema_version":1,"projects":[]}`), digest([]byte(`{"schema_version":1,"projects":[]}`))},
  {"unknown schema", []byte(`{"schema_version":2,"projects":[]}`), digest([]byte(`{"schema_version":2,"projects":[]}`))},
 }
 for _, tc := range cases { t.Run(tc.name,func(t *testing.T){
  if _,err:=decodePerspectiveSchedulerDeployment(tc.raw,tc.pin);err==nil {t.Fatal("untrusted deployment accepted")}
 }) }
}

// This unit contract guards the installation inventory, not deployed SQL semantics.
func TestPerspectiveReplacementBootstrapRequiresPreviewAuthority(t *testing.T) {
 required:=perspectiveReplacementRequiredFunctions()
 seen:=make(map[string]bool,len(required))
 for _,signature:=range required {
  if signature==""||seen[signature]{t.Fatal("invalid installation inventory",signature)}
  seen[signature]=true
 }
 for _,signature:=range []string{
  "photo.read_v29_perspective_preview_replacement_identity(uuid,uuid)",
  "photo.v29_perspective_preview_original_array(uuid,uuid,uuid,integer,bigint,text,uuid,text,text)",
 } {
  if !seen[signature]{t.Fatal("replacement preview prerequisite missing",signature)}
 }
 first:=required[0];required[0]="changed"
 if perspectiveReplacementRequiredFunctions()[0]!=first{t.Fatal("caller changed startup authority inventory")}
}
