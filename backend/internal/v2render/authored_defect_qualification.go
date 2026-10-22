package v2render

import (
 "context"
 "math"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/tenant"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)

// AuthoredDefectQualification retains real imported report evidence; neither
// this proof nor the pure task compiler grants admission or a repair decision.
type AuthoredDefectQualification struct {
 tenant,project,actor,session uuid.UUID
 expected string
 report v2input.Ref
 object objectstore.Object
 costs v2input.SemanticQualifiedCosts
}
type SQLAuthoredDefectQualification struct {
 API *pgxpool.Pool
 Objects v2input.SessionAnalysisObjectReader
 Bucket string
}
func(s SQLAuthoredDefectQualification)lookup(ctx context.Context,who auth.Identity,tenantID,project uuid.UUID,expected v2qualification.Expected,report v2input.Ref)(objectstore.Object,error){
 empty:=objectstore.Object{}
 if err:=ctx.Err();err!=nil{return empty,err}
 if s.API==nil||s.Objects==nil||s.Bucket==""||tenantID==uuid.Nil||project==uuid.Nil||who.UserID==uuid.Nil||who.SessionID==uuid.Nil||expected.Operation!="defects.propose"||expected.Version!="1"||expected.Backend!="cpu"||!validLayerArtifactRef(report){return empty,v2qualification.ErrUnqualified}
 body,err:=canonical.Marshal(expected);if err!=nil{return empty,err}
 reportBody,err:=canonical.Marshal(report);if err!=nil{return empty,err}
 var out objectstore.Object
 err=tenant.WithTenant(ctx,s.API,who,tenantID,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  rows,err:=tx.Query(ctx,`SELECT artifact_id,sha256,bucket,object_key,object_version,bytes FROM photo.semantic_proposal_defect_qualification_object($1,$2,$3,$4,$5,$6)`,tenantID,project,who.UserID,who.SessionID,body,reportBody)
  if err!=nil{return err};defer rows.Close();count:=0
  for rows.Next(){var ref v2input.Ref;var bucket string;var obj objectstore.Object
   if err=rows.Scan(&ref.ArtifactID,&ref.SHA256,&bucket,&obj.Key,&obj.VersionID,&obj.Bytes);err!=nil{return err}
   count++;if count!=1||ref!=report||bucket!=s.Bucket||obj.Bytes<2||obj.Bytes>8<<20||obj.VersionID==""||obj.VersionID=="null"{return v2qualification.ErrUnqualified}
   obj.SHA256=ref.SHA256;obj.ContentType="application/json";out=obj
  }
  if err=rows.Err();err!=nil{return err};if count!=1{return v2qualification.ErrUnqualified};return nil
 });if err!=nil{return empty,err};return out,nil
}
func authoredDefectMeasuredCosts(metrics v2qualification.Metrics)(v2input.SemanticQualifiedCosts,error){
 for _,n:=range []float64{metrics.PeakRSSBytes,metrics.PeakVRAMBytes,metrics.ScratchBytes}{if math.IsNaN(n)||math.IsInf(n,0)||n<0||n>9007199254740991{return v2input.SemanticQualifiedCosts{},v2input.ErrInvalid}}
 if metrics.PeakVRAMBytes!=0{return v2input.SemanticQualifiedCosts{},v2qualification.ErrUnqualified}
 return v2input.SemanticQualifiedCosts{RAMBytes:int64(math.Ceil(metrics.PeakRSSBytes)),VRAMBytes:0,ScratchBytes:int64(math.Ceil(metrics.ScratchBytes))},nil
}
func(s SQLAuthoredDefectQualification)Read(ctx context.Context,who auth.Identity,tenantID,project uuid.UUID,expected v2qualification.Expected,report v2input.Ref)(AuthoredDefectQualification,error){
 empty:=AuthoredDefectQualification{}
 expected=ownAuthoredDefectExpected(expected)
 before,err:=s.lookup(ctx,who,tenantID,project,expected,report);if err!=nil{return empty,err}
 raw,err:=v2input.ReadPinnedSemanticDocument(ctx,s.Objects,before,report);if err!=nil{return empty,err}
 verified,err:=v2qualification.Verify(raw,report.SHA256,expected);if err!=nil{return empty,err}
 costs,err:=authoredDefectMeasuredCosts(verified.Metrics);if err!=nil{return empty,err}
 after,err:=s.lookup(ctx,who,tenantID,project,expected,report);if err!=nil{return empty,err}
 if before!=after{return empty,v2input.ErrConflict}
 wire,err:=canonical.Marshal(expected);if err!=nil{return empty,err}
 if err=ctx.Err();err!=nil{return empty,err}
 return AuthoredDefectQualification{tenant:tenantID,project:project,actor:who.UserID,session:who.SessionID,expected:string(wire),report:report,object:before,costs:costs},nil
}
func(s SQLAuthoredDefectQualification)Recheck(ctx context.Context,who auth.Identity,tenantID,project uuid.UUID,expected v2qualification.Expected,proof AuthoredDefectQualification)error{
 expected=ownAuthoredDefectExpected(expected)
 if err:=ctx.Err();err!=nil{return err}
 wire,err:=canonical.Marshal(expected);if err!=nil{return err}
 if proof.tenant!=tenantID||proof.project!=project||proof.actor!=who.UserID||proof.session!=who.SessionID||proof.expected!=string(wire){return v2input.ErrInvalid}
 current,err:=s.lookup(ctx,who,tenantID,project,expected,proof.report);if err!=nil{return err}
 if current!=proof.object{return v2input.ErrConflict};return ctx.Err()
}

// Own the deployment-controlled policy before any lookup or physical IO; a
// caller mutating its configuration later cannot change the verified snapshot.
func ownAuthoredDefectExpected(value v2qualification.Expected) v2qualification.Expected {
 if value.MPSResources!=nil { owned:=*value.MPSResources;value.MPSResources=&owned }
 value.RequiredChecks=append([]string(nil),value.RequiredChecks...)
 copySet:=func(src map[string]struct{})map[string]struct{}{
  if src==nil{return nil};out:=make(map[string]struct{},len(src));for k:=range src{out[k]=struct{}{}};return out
 }
 value.CalibrationSHAs=copySet(value.CalibrationSHAs)
 value.TestFixtureSHAs=copySet(value.TestFixtureSHAs)
 value.EvidenceSHAs=copySet(value.EvidenceSHAs)
 return value
}
