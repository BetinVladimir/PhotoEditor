package workflow

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "errors"
 "strings"
 "testing"

 "photoeditor/backend/internal/v2optical"
)

// These refusal tests do not establish deployment or database qualification.
func TestNegativeSchedulerDeploymentRefusesUntrustedConfiguration(t *testing.T) {
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
  if _,err:=decodeNegativeSchedulerDeployment(tc.raw,tc.pin);err==nil {t.Fatal("untrusted deployment accepted")}
 }) }
}

func TestNegativeReplacementBootstrapWithoutConfigurationPreservesBase(t *testing.T) {
 t.Setenv("PHOTO_V29_PERSPECTIVE_CONFIG","")
 t.Setenv("PHOTO_V29_NEGATIVE_CONFIG","")
 backend,err:=buildNegativeReplacementScheduler(context.Background(),nil,nil,"")
 if err!=nil {t.Fatal(err)}
 if _,ok:=backend.(v2optical.SQLNegativeScheduler);!ok {t.Fatalf("unexpected backend %T",backend)}
}

func TestNegativeReplacementBootstrapConfiguredDependenciesRequired(t *testing.T) {
 t.Setenv("PHOTO_V29_PERSPECTIVE_CONFIG","")
 t.Setenv("PHOTO_V29_NEGATIVE_CONFIG","/unavailable/negative.json")
 backend,err:=buildNegativeReplacementScheduler(context.Background(),nil,nil,"photos")
 if err==nil||backend!=nil {t.Fatal("configured deployment accepted without dependencies")}
 if err.Error()!="negative replacement dependencies unavailable" {t.Fatalf("unexpected refusal: %v",err)}
}

func TestNegativeReplacementBootstrapCancellationPrecedesFallback(t *testing.T) {
 t.Setenv("PHOTO_V29_PERSPECTIVE_CONFIG","")
 t.Setenv("PHOTO_V29_NEGATIVE_CONFIG","")
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 backend,err:=buildNegativeReplacementScheduler(ctx,nil,nil,"")
 if !errors.Is(err,context.Canceled)||backend!=nil {t.Fatalf("cancellation ignored: %T %v",backend,err)}
}
