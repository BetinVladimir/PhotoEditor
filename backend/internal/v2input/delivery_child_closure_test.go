package v2input

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"testing"
)

func TestDeliveryChildClosureOwnsInventoryAndRequiresTypedRoots(t *testing.T) {
	root := func(contract string, n int) Entry {
		return Entry{Ref: Ref{ArtifactID: uuid.New(), SHA256: fmt.Sprintf("%064x", n)}, Contract: contract}
	}
	roots := []Entry{root("WorkingImage", 1), root("NpyArray", 2), root("EditingRecipeV3", 3), root("ExportPresetConfig", 4)}
	paramsRaw, err := canonical.Marshal(map[string]any{"image": roots[0].Ref})
	if err != nil {
		t.Fatal(err)
	}
	sha, err := canonical.Hash(json.RawMessage(paramsRaw))
	if err != nil {
		t.Fatal(err)
	}
	params := Ref{ArtifactID: uuid.New(), SHA256: sha}
	workflow := uuid.New()
	resolved := append([]Entry(nil), roots...)
	for i := 0; i < 200; i++ {
		resolved = append(resolved, root("NpyArray", i+100))
	}
	closure, err := CompileDeliveryChildClosure(workflow, "export.encode", paramsRaw, params, roots, resolved)
	if err != nil {
		t.Fatal(err)
	}
	if len(closure.Direct) != len(roots)+1 {
		t.Fatal("transitive entries promoted to direct roots")
	}
	fullCursor, _ := closure.Open(context.Background())
	count := 0
	for {
		_, ok, err := fullCursor.Next(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			break
		}
		count++
	}
	if count != len(resolved)+1 {
		t.Fatal("transitive inventory lost", count)
	}
	original := closure.InputDigest
	resolved[0] = Entry{}
	paramsRaw[0] = 'x'
	if closure.InputDigest != original || closure.ParamsJCS[0] != '{' {
		t.Fatal("borrowed input")
	}
	cursor, err := closure.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if cursor == nil {
		t.Fatal("missing cursor")
	}
	if _, err := CompileDeliveryChildClosure(workflow, "export.encode", closure.ParamsJCS, params, roots, resolved); err == nil {
		t.Fatal("missing root accepted")
	}
	bad := params
	bad.SHA256 = fmt.Sprintf("%064x", 99)
	if _, err := CompileDeliveryChildClosure(workflow, "export.encode", closure.ParamsJCS, bad, roots, roots); err == nil {
		t.Fatal("foreign params accepted")
	}
	if _, err := CompileDeliveryChildClosure(workflow, "image.relight", closure.ParamsJCS, params, roots, roots); err == nil {
		t.Fatal("foreign operation accepted")
	}
}

func TestDeliveryWatermarkClosureRequiresBothImageRoots(t *testing.T){
 root:=func(contract string,n int)Entry{return Entry{Ref:Ref{ArtifactID:uuid.New(),SHA256:fmt.Sprintf("%064x",n)},Contract:contract}}
 roots:=[]Entry{root("WorkingImage",1),root("NpyArray",2),root("WorkingImage",3),root("NpyArray",4),root("NpyArray",5),root("EditingRecipeV3",6),root("ExportPresetConfig",7)}
 raw,err:=canonical.Marshal(map[string]any{"image":roots[0].Ref,"watermark":roots[2].Ref});if err!=nil{t.Fatal(err)}
 hash,err:=canonical.Hash(json.RawMessage(raw));if err!=nil{t.Fatal(err)};params:=Ref{ArtifactID:uuid.New(),SHA256:hash};workflow:=uuid.New()
 closure,err:=CompileDeliveryWatermarkClosure(workflow,raw,params,roots,roots);if err!=nil{t.Fatal(err)}
 if len(closure.Direct)!=len(roots)+1{t.Fatal("lost worker roots")}
 for _,mode:=range []string{"missing_mark","transitive_only","wrong_contract","substituted_sha"}{t.Run(mode,func(t *testing.T){
  direct:=append([]Entry(nil),roots...);resolved:=append([]Entry(nil),roots...)
  switch mode{
  case "missing_mark":resolved=append(resolved[:2],resolved[3:]...)
  case "transitive_only":direct=append(direct[:2],direct[3:]...)
  case "wrong_contract":direct[2].Contract="NpyArray";resolved[2]=direct[2]
  case "substituted_sha":resolved[2].Ref.SHA256=fmt.Sprintf("%064x",99)
  }
  if _,err:=CompileDeliveryWatermarkClosure(workflow,raw,params,direct,resolved);err==nil{t.Fatal("unbound watermark accepted",mode)}
 })}
 if _,err:=CompileDeliveryChildClosure(workflow,"derivative.watermark",raw,params,roots,roots);err==nil{t.Fatal("ordinary closure bypass")}
}
