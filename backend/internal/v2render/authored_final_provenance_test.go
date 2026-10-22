package v2render

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)

func TestAuthoredFinalOriginalGeometryUsesOrdinaryBootstrap(t *testing.T){
 ref:=v2input.Ref{ArtifactID:uuid.New(),SHA256:"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
 catalog:=SQLAuthoredFinalCatalog{originalEntries:map[v2input.Entry]bool{{Ref:ref,Contract:"WorkingImage"}:true}}
 // No scheduler/object reader is configured. The original typed entry must
 // return the ordinary-bootstrap signal before attempting authored SQL/IO.
 _,found,err:=catalog.Geometry(context.Background(),auth.Identity{},uuid.New(),v2draft.LayerRef{Ref:ref,Contract:"WorkingImage"})
 if err!=nil||found{t.Fatalf("original bootstrap was replaced: found=%v err=%v",found,err)}
}

func TestAuthoredFinalAllCompilerRootsRequireExactTypedInventory(t *testing.T){
 ref:=v2input.Ref{ArtifactID:uuid.New(),SHA256:"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
 required:=[]v2input.Entry{{Ref:ref,Contract:"WorkingImage"}}
 if !authoredFinalCompilerRootsRetained(required,required){t.Fatal("exact root refused")}
 for _,entries:=range [][]v2input.Entry{
  nil,
  {{Ref:ref,Contract:"Artifact"}},
  {{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:ref.SHA256},Contract:"WorkingImage"}},
  {{Ref:v2input.Ref{ArtifactID:ref.ArtifactID,SHA256:"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},Contract:"WorkingImage"}},
 }{
  if authoredFinalCompilerRootsRetained(required,entries){t.Fatal("missing or substituted precise root admitted")}
 }
}
