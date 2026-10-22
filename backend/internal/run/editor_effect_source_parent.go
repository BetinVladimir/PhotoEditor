package run

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "errors"
 "io"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/tenant"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)

// The SQL reader binds this root to the accepted master's exact canonical
// producer generation. Matching WorkingImage bytes alone never grant authority.
func(s EditorEffectsService)sourceParent(ctx context.Context,who auth.Identity,tid,pid uuid.UUID,binding v2input.SessionVersionBinding)(*v2input.Ref,[]v2input.Ref,error){
 var ref v2input.Ref;var obj objectstore.Object;var closureRaw []byte;var bucket string;found:=false
 err:=tenant.WithTenant(ctx,s.Runs.DB,who,tid,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  e:=tx.QueryRow(ctx,`SELECT * FROM photo.editor_effect_source_parent($1,$2,$3,$4,$5,$6,$7)`,tid,pid,binding.AssetVersionID,binding.Source.ArtifactID,binding.Source.SHA256,who.UserID,who.SessionID).Scan(&ref.ArtifactID,&ref.SHA256,&bucket,&obj.Key,&obj.VersionID,&obj.Bytes,&obj.ContentType,&closureRaw)
  if errors.Is(e,pgx.ErrNoRows){return nil};found=e==nil;return e
 });if err!=nil||!found{return nil,nil,err}
 invalid:=func()(*v2input.Ref,[]v2input.Ref,error){return nil,nil,domain.NewError("EDITOR_SOURCE_PARENT_INVALID",409)}
 if bucket!=s.Drafts.Bucket||ref.ArtifactID==uuid.Nil||!isLowerHexSHA(ref.SHA256)||obj.ContentType!="application/json"||obj.Bytes<2||obj.Bytes>1<<20||obj.VersionID==""||obj.VersionID=="null"{return invalid()};obj.SHA256=ref.SHA256
 reader,err:=s.Drafts.Objects.Open(ctx,obj);if err!=nil{return nil,nil,err};if reader==nil{return invalid()}
 raw,readErr:=io.ReadAll(io.LimitReader(reader,obj.Bytes+1));closeErr:=reader.Close();if readErr!=nil{return nil,nil,readErr};if closeErr!=nil{return nil,nil,closeErr};sum:=sha256.Sum256(raw);if int64(len(raw))!=obj.Bytes||hex.EncodeToString(sum[:])!=ref.SHA256{return invalid()}
 parent,err:=v2draft.ParseImageResult(raw,ref);if err!=nil||parent.WorkingImage!=binding.Source{return invalid()}
 var closure []v2input.Ref;if json.Unmarshal(closureRaw,&closure)!=nil||len(closure)==0||len(closure)>4096{return invalid()}
 seen:=map[uuid.UUID]string{};for _,item:=range closure{if item.ArtifactID==uuid.Nil||!isLowerHexSHA(item.SHA256){return invalid()};if prior,ok:=seen[item.ArtifactID];ok&&prior!=item.SHA256{return invalid()};seen[item.ArtifactID]=item.SHA256}
 if seen[ref.ArtifactID]!=ref.SHA256||seen[binding.Source.ArtifactID]!=binding.Source.SHA256{return invalid()}
 if err=ctx.Err();err!=nil{return nil,nil,err};return &ref,closure,nil
}
