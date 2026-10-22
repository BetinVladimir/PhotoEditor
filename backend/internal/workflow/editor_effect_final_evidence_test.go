package workflow

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"testing"
	"github.com/google/uuid"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type editorFinalReadFixture struct{body []byte;closeErr error}
type editorFinalReader struct{*bytes.Reader;closeErr error}
func(r editorFinalReader)Close()error{return r.closeErr}
func(f editorFinalReadFixture)Open(context.Context,objectstore.Object)(io.ReadCloser,error){return editorFinalReader{bytes.NewReader(f.body),f.closeErr},nil}

func TestEditorFinalReadRejectsChangedPhysicalBytesAndCloseFailure(t *testing.T){
	body:=[]byte(`{"schema_version":1}`);sum:=sha256.Sum256(body)
	object:=objectstore.Object{Key:"immutable",VersionID:"version",SHA256:hex.EncodeToString(sum[:]),Bytes:int64(len(body)),ContentType:"application/json"}
	if raw,err:=readEditorFinalDocument(context.Background(),editorFinalReadFixture{body:body},object,1024);err!=nil||!bytes.Equal(raw,body){t.Fatal("exact physical bytes failed",err)}
	for _,fixture:=range []editorFinalReadFixture{{body:[]byte(`{"schema_version":2}`)},{body:body,closeErr:errors.New("close")}}{
		if _,err:=readEditorFinalDocument(context.Background(),fixture,object,1024);err==nil{t.Fatal("failed physical proof accepted")}
	}
}

func TestEditorFinalEvidenceMergeRejectsConflictingPinsAndDuplicatePaths(t *testing.T){
	ref:=v2input.Ref{ArtifactID:uuid.New(),SHA256:hex.EncodeToString(make([]byte,32))}
	branch:=v2draft.TaskInputEvidence{Entries:[]v2input.Entry{{Ref:ref,Contract:"WorkingImage"}},Occurrences:[]v2draft.OccurrenceWitness{{Node:v2draft.LayerRef{Path:"original.source",Ref:ref}}}}
	if _,err:=mergeEditorFinalEvidence(branch,branch);err==nil{t.Fatal("duplicate semantic occurrence path accepted")}
	changed:=branch;changed.Occurrences=nil;changed.Entries=[]v2input.Entry{{Ref:ref,Contract:"MaskRef"}}
	if _,err:=mergeEditorFinalEvidence(branch,changed);err==nil{t.Fatal("same artifact retyped as a different document")}
}
