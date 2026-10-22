package v2input

import (
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
)

type LensCatalogEntry struct {
 Profile Ref `json:"profile"`
 CameraModel string `json:"camera_model"`
 LensModel string `json:"lens_model"`
 FocalMM float64 `json:"focal_mm"`
 Aperture float64 `json:"aperture"`
}
type LensCatalogPage struct { Items []LensCatalogEntry `json:"items"`; Next *uuid.UUID `json:"next,omitempty"` }
type LensCatalog struct { DB LensSelectionQuerier; Scheduler AnalysisStageQuerier; Source SQLLensProfileSource }
func lensCatalogEntry(ref Ref,p LensProfileDocument)LensCatalogEntry {return LensCatalogEntry{ref,p.CameraModel,p.LensModel,p.FocalMM,p.Aperture}}

func(c LensCatalog) Register(ctx context.Context,who auth.Identity,tenant,project uuid.UUID,ref Ref)(LensCatalogEntry,error){
 if c.Scheduler==nil || who.UserID==uuid.Nil || who.SessionID==uuid.Nil {return LensCatalogEntry{},ErrInvalid}
 raw,err:=c.Source.Load(ctx,tenant,project,who.UserID,who.SessionID,ref);if err!=nil {return LensCatalogEntry{},err}
 p,err:=ParseLensProfile(raw);if err!=nil{return LensCatalogEntry{},err}
 var coeff map[string]json.RawMessage
 if json.Unmarshal(raw,&coeff)!=nil{return LensCatalogEntry{},ErrInvalid};delete(coeff,"qualification")
 sha,err:=canonical.Hash(coeff);if err!=nil{return LensCatalogEntry{},err}
 var accepted bool
 err=c.Scheduler.QueryRow(ctx,`SELECT photo.register_v25_lens_profile($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,tenant,project,who.UserID,who.SessionID,ref.ArtifactID,ref.SHA256,p.Qualification.ArtifactID,p.Qualification.SHA256,sha,c.Source.RuntimeLockSHA).Scan(&accepted)
 if err!=nil{return LensCatalogEntry{},err};if !accepted{return LensCatalogEntry{},ErrExpired}
 // Revalidate authority after the write before exposing a successful response.
 if _,err=c.Source.object(ctx,tenant,project,who.UserID,who.SessionID,ref,1048576);err!=nil{return LensCatalogEntry{},err}
 if err=ctx.Err();err!=nil{return LensCatalogEntry{},err}
 return lensCatalogEntry(ref,p),nil
}
func(c LensCatalog) List(ctx context.Context,who auth.Identity,tenant,project uuid.UUID,after *uuid.UUID)(LensCatalogPage,error){
 empty:=LensCatalogPage{}
 if c.DB==nil || who.UserID==uuid.Nil || who.SessionID==uuid.Nil || tenant==uuid.Nil || project==uuid.Nil || (after!=nil && *after==uuid.Nil){return empty,ErrInvalid}
 rows,err:=c.DB.Query(ctx,`SELECT profile_id,profile_sha FROM photo.list_v25_lens_profiles($1,$2,$3,$4,$5)`,tenant,project,who.UserID,who.SessionID,after)
 if err!=nil{return empty,err}
 refs:=[]Ref{};seen:=map[uuid.UUID]bool{}
 for rows.Next(){var ref Ref;if err=rows.Scan(&ref.ArtifactID,&ref.SHA256);err!=nil{rows.Close();return empty,err}
  if len(refs)>=101 || !validRef(ref) || seen[ref.ArtifactID] || (after!=nil && ref.ArtifactID.String()<=after.String()) || (len(refs)>0 && refs[len(refs)-1].ArtifactID.String()>=ref.ArtifactID.String()){rows.Close();return empty,ErrInvalid}
  seen[ref.ArtifactID]=true;refs=append(refs,ref)
 };err=rows.Err();rows.Close();if err!=nil{return empty,err}
 page:=LensCatalogPage{Items:[]LensCatalogEntry{}}
 if len(refs)>100 {next:=refs[99].ArtifactID;page.Next=&next;refs=refs[:100]}
 for _,ref:=range refs {
  raw,e:=c.Source.Load(ctx,tenant,project,who.UserID,who.SessionID,ref)
  if e!=nil{return empty,e} // Never present unverified rows as qualified options.
  p,e:=ParseLensProfile(raw);if e!=nil{return empty,e}
  page.Items=append(page.Items,lensCatalogEntry(ref,p))
 }
 if err=ctx.Err();err!=nil{return empty,err}
 return page,nil
}
