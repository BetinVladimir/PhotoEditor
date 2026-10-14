package interchange

import (
 "errors"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

func TestWarpOccurrenceAuthority(t *testing.T){
 scope:=preparedPublisherScope();scope.Contract="DenseWarpField"
 canvas:=review.Canvas{Width:3,Height:2,Orientation:1,SpaceID:"original",SourceSHA256:rawSHA([]byte("image"))}
 scope.Warp=&ImportWarpOccurrence{Original:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("warp"))},From:canvas,To:canvas}
 digest,err:=canonical.Hash(struct{From,To review.Canvas;Target v2input.SessionVersionBinding}{canvas,canvas,scope.Binding});if err!=nil{t.Fatal(err)}
 ref:=v2input.Ref{ArtifactID:uuid.NewSHA1(scope.Workflow,[]byte("interchange:warp:"+scope.Entry.String()+":"+scope.Warp.Original.ArtifactID.String()+":"+digest)),SHA256:scope.Warp.Original.SHA256}
 if err:=validateImportWarpOccurrence(scope,ref);err!=nil{t.Fatal(err)}
 cases:=[]struct{name string;mutate func(*ImportPreparedScope,*v2input.Ref)}{
  {"contract",func(s *ImportPreparedScope,r *v2input.Ref){s.Contract="MaskPixels"}},
  {"identity",func(s *ImportPreparedScope,r *v2input.Ref){r.ArtifactID=uuid.New()}},
  {"content",func(s *ImportPreparedScope,r *v2input.Ref){r.SHA256=rawSHA([]byte("foreign"))}},
  {"original",func(s *ImportPreparedScope,r *v2input.Ref){s.Warp.Original.ArtifactID=uuid.Nil}},
  {"geometry",func(s *ImportPreparedScope,r *v2input.Ref){s.Warp.To.Width=0}},
  {"pixel limit",func(s *ImportPreparedScope,r *v2input.Ref){s.Warp.To.Width=200000000;s.Warp.To.Height=2}},
  {"orientation",func(s *ImportPreparedScope,r *v2input.Ref){s.Warp.From.Orientation=2}},
  {"space",func(s *ImportPreparedScope,r *v2input.Ref){s.Warp.To.SpaceID="bad space"}},
  {"lineage",func(s *ImportPreparedScope,r *v2input.Ref){s.Warp.To.SourceSHA256=rawSHA([]byte("other"))}},
  {"target",func(s *ImportPreparedScope,r *v2input.Ref){s.Binding.CanvasHash=rawSHA([]byte("other canvas"))}},
  {"workflow",func(s *ImportPreparedScope,r *v2input.Ref){s.Workflow=uuid.New()}},
 }
 for _,c:=range cases{t.Run(c.name,func(t *testing.T){
  candidate:=scope;owned:=*scope.Warp;candidate.Warp=&owned;changed:=ref;c.mutate(&candidate,&changed)
  if err:=validateImportWarpOccurrence(candidate,changed);!errors.Is(err,v2input.ErrInvalid){t.Fatal("invalid authority accepted",err)}
 })}
 scope.Warp=nil
 if err:=validateImportWarpOccurrence(scope,ref);err!=nil{t.Fatal("ordinary binary receipt rejected",err)}
}
