package workflow
import("testing";"context";"github.com/google/uuid";"photoeditor/backend/internal/v2input";"photoeditor/backend/internal/artifactjson";"photoeditor/backend/internal/objectstore")
func TestEditorFinalRootsRequireOneOfEach(t *testing.T){
 recipe:=v2input.Ref{ArtifactID:uuid.New(),SHA256:"recipe"};source:=v2input.Ref{ArtifactID:uuid.New(),SHA256:"source"}
 r:=v2input.Entry{Ref:recipe,Contract:"FrozenRenderRecipe"};s:=v2input.Entry{Ref:source,Contract:"WorkingImage"}
 for _,pair:=range [][]v2input.Entry{{r,r},{s,s},{r},{r,s,s},{r,{Ref:source,Contract:"Artifact"}}}{if validEditorFinalRoots(pair,recipe,source){t.Fatal("invalid roots admitted")}}
 if !validEditorFinalRoots([]v2input.Entry{r,s},recipe,source)||!validEditorFinalRoots([]v2input.Entry{s,r},recipe,source){t.Fatal("exact roots rejected")}
}
func TestEditorFinalDocumentCancelledBeforeDatabase(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());cancel();repo:=editorFinalDocuments{}
 if _,err:=repo.Reserve(ctx,artifactjson.Scope{},"",0);err!=context.Canceled{t.Fatal(err)}
 if _,err:=repo.Complete(ctx,artifactjson.Scope{},artifactjson.Reservation{},objectstore.Object{});err!=context.Canceled{t.Fatal(err)}
}
