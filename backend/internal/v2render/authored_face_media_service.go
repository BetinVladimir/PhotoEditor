package v2render

import (
 "bytes"
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "image"
 _ "image/jpeg"
 "io"
 "time"
 _ "golang.org/x/image/webp"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/tenant"
 "photoeditor/backend/internal/v2input"
)

type AuthoredFaceMediaService struct { API *pgxpool.Pool; Objects v2input.SessionAnalysisObjectReader; Bucket, SRGBSHA, WorkingSHA string }
type authoredFaceMediaLocation struct { Ref v2input.Ref `json:"ref"`; Bucket string `json:"bucket"`; Object objectstore.Object `json:"object"` }
type authoredFaceMediaCatalog struct {
 CandidateSHA string `json:"candidate_sha256"`
 ProfileHash string `json:"profile_hash"`
 Candidate AuthoredFaceSelectionCandidate `json:"candidate"`
 Run uuid.UUID `json:"run_id"`; Task uuid.UUID `json:"task_id"`; Result uuid.UUID `json:"result_id"`
 Attempt int `json:"attempt"`; Fence int64 `json:"fence"`
 Image authoredFaceMediaLocation `json:"image"`; Manifest authoredFaceMediaLocation `json:"manifest"`; Derivative authoredFaceMediaLocation `json:"derivative"`
}
// The SQL DTO is strict, bounded and owned before any physical IO.
func decodeAuthoredFaceMediaCatalog(raw []byte)(authoredFaceMediaCatalog,error){
 var value authoredFaceMediaCatalog
 if len(raw)<2||len(raw)>12<<20{return value,v2input.ErrInvalid}
 d:=json.NewDecoder(bytes.NewReader(raw));d.DisallowUnknownFields()
 if d.Decode(&value)!=nil||!authoredFinalDigest(value.ProfileHash){return value,v2input.ErrInvalid}
 var extra any;if d.Decode(&extra)!=io.EOF{return value,v2input.ErrInvalid}
 return value,nil
}
// The private fields prevent a route caller from manufacturing readback proof or
// substituting bytes between physical validation and the final SQL recheck.
type AuthoredFaceMedia struct {
 raw []byte; mime string; width,height int
 catalog []byte; who auth.Identity; tenant,parent uuid.UUID; ordinal int; sha string
}
func (v AuthoredFaceMedia) Bytes() []byte { return bytes.Clone(v.raw) }
func (v AuthoredFaceMedia) ContentType() string { return v.mime }
func (v AuthoredFaceMedia) Dimensions() (int,int) { return v.width,v.height }
func (s AuthoredFaceMediaService) catalog(ctx context.Context,who auth.Identity,tid,parent uuid.UUID,ordinal int,sha string)(authoredFaceMediaCatalog,[]byte,error){
 var value authoredFaceMediaCatalog;var raw []byte
 if s.API==nil||tid==uuid.Nil||parent==uuid.Nil||who.UserID==uuid.Nil||who.SessionID==uuid.Nil||ordinal<1||ordinal>4998||!authoredFinalDigest(sha){return value,nil,v2input.ErrInvalid}
 err:=tenant.WithTenant(ctx,s.API,who,tid,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  return tx.QueryRow(ctx,"SELECT photo.read_authored_face_preview_media($1,$2,$3,$4,$5,$6)",tid,parent,ordinal,sha,who.UserID,who.SessionID).Scan(&raw)
 })
 if err!=nil{return value,nil,err};if len(raw)<2||len(raw)>12<<20{return value,nil,v2input.ErrExpired}
 value,err=decodeAuthoredFaceMediaCatalog(raw);if err!=nil{return value,nil,err}
 owned,err:=canonical.Marshal(value);if err!=nil{return value,nil,err}
 candidate,err:=canonical.Marshal(value.Candidate);if err!=nil{return value,nil,err}
 sum:=sha256.Sum256(candidate)
 run:=uuid.NewSHA1(value.Candidate.Instruction,[]byte("AuthoredFacePreview/v1:"+sha))
 if value.CandidateSHA!=sha||hex.EncodeToString(sum[:])!=sha||value.Candidate.Tenant!=tid||value.Candidate.Parent!=parent||value.Candidate.Ordinal!=ordinal||value.Run!=run||value.Task!=uuid.NewSHA1(run,[]byte("authored.face:preview1600"))||value.Result==uuid.Nil||value.Attempt<1||value.Fence<1{return value,nil,v2input.ErrInvalid}
 for _,loc:=range []authoredFaceMediaLocation{value.Image,value.Manifest,value.Derivative}{
  if loc.Ref.ArtifactID==uuid.Nil||!authoredFinalDigest(loc.Ref.SHA256)||loc.Bucket!=s.Bucket||loc.Object.Key==""||loc.Object.VersionID==""||loc.Object.VersionID=="null"||loc.Object.SHA256!=loc.Ref.SHA256||loc.Object.Bytes<2{return value,nil,v2input.ErrInvalid}
 }
 if value.Image.Ref!=value.Candidate.Source.Source{return value,nil,v2input.ErrInvalid}
 return value,owned,nil
}
var authoredFaceMediaSlots=make(chan struct{},4)
func (s AuthoredFaceMediaService) Read(ctx context.Context,who auth.Identity,tid,parent uuid.UUID,ordinal int,sha string)(AuthoredFaceMedia,error){
 var zero AuthoredFaceMedia
 ctx,cancel:=context.WithTimeout(ctx,30*time.Second);defer cancel()
 // Authorization/catalog happens before reporting unavailable physical storage.
 c,snapshot,err:=s.catalog(ctx,who,tid,parent,ordinal,sha);if err!=nil{return zero,err}
 if s.Objects==nil||!authoredFinalDigest(s.WorkingSHA)||!authoredFinalDigest(s.SRGBSHA)||len(s.Bucket)<3{return zero,domain.NewError("DEPENDENCY_UNAVAILABLE",503)}
 select{case authoredFaceMediaSlots<-struct{}{}:defer func(){<-authoredFaceMediaSlots}();case <-ctx.Done():return zero,ctx.Err()}
 imageRaw,err:=v2input.ReadPinnedSemanticDocument(ctx,s.Objects,c.Image.Object,c.Image.Ref);if err!=nil{return zero,err}
 wi,err:=review.ParseWorkingImage(imageRaw);if err!=nil{return zero,err};canvas,err:=canonical.Hash(wi.Canvas);if err!=nil||canvas!=c.Candidate.Source.CanvasHash||wi.Color.ICCSHA256!=s.WorkingSHA{return zero,v2input.ErrInvalid}
 manifestRaw,err:=v2input.ReadPinnedSemanticDocument(ctx,s.Objects,c.Manifest.Object,c.Manifest.Ref);if err!=nil{return zero,err}
 manifest,err:=review.ParseRenderDerivativeManifest(manifestRaw,review.ArtifactRef{ID:c.Manifest.Ref.ArtifactID,SHA256:c.Manifest.Ref.SHA256},review.ArtifactRef{ID:c.Image.Ref.ArtifactID,SHA256:c.Image.Ref.SHA256},review.ArtifactRef{ID:c.Derivative.Ref.ArtifactID,SHA256:c.Derivative.Ref.SHA256},wi,"preview1600",s.SRGBSHA);if err!=nil{return zero,err}
 if manifest.ProfileHash!=c.ProfileHash||c.Derivative.Object.ContentType!=manifest.MediaType||c.Derivative.Object.Bytes>52<<20{return zero,v2input.ErrInvalid}
 reader,err:=s.Objects.Open(ctx,c.Derivative.Object);if err!=nil{return zero,err}
 raw,readErr:=io.ReadAll(io.LimitReader(reader,c.Derivative.Object.Bytes+1));closeErr:=reader.Close();if readErr!=nil{return zero,readErr};if closeErr!=nil{return zero,closeErr}
 sum:=sha256.Sum256(raw);if int64(len(raw))!=c.Derivative.Object.Bytes||hex.EncodeToString(sum[:])!=c.Derivative.Ref.SHA256{return zero,v2input.ErrInvalid}
 config,format,err:=image.DecodeConfig(bytes.NewReader(raw));expected:="jpeg";if manifest.Alpha{expected="webp"};if err!=nil||format!=expected||config.Width!=manifest.Width||config.Height!=manifest.Height{return zero,v2input.ErrInvalid}
 result:=AuthoredFaceMedia{raw:bytes.Clone(raw),mime:manifest.MediaType,width:manifest.Width,height:manifest.Height,catalog:bytes.Clone(snapshot),who:who,tenant:tid,parent:parent,ordinal:ordinal,sha:sha}
 if err=s.Recheck(ctx,result);err!=nil{return zero,err};return result,nil
}
// Repeat immediately before headers; no URL/token may retain historical access.
func (s AuthoredFaceMediaService) Recheck(ctx context.Context,value AuthoredFaceMedia)error{
 if len(value.raw)<2||len(value.catalog)<2{return v2input.ErrInvalid}
 _,current,err:=s.catalog(ctx,value.who,value.tenant,value.parent,value.ordinal,value.sha);if err!=nil{return err};if !bytes.Equal(current,value.catalog){return v2input.ErrExpired};return ctx.Err()
}
