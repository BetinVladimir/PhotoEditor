package delivery

import (
 "encoding/json"
 "errors"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)

func TestDelegatedReadyExportRequiresExactScopedVersion(t *testing.T){
 scope:=SignerDelegationScope{TenantID:uuid.New(),ProjectID:uuid.New(),
  ExportID:uuid.New(),ExportArtifactID:uuid.New(),
  ExportSHA:signingObjectHash([]byte("export"))}
 evidence:=delegatedExportEvidence{TenantID:scope.TenantID,ProjectID:scope.ProjectID,
  ExportID:scope.ExportID,VersionID:uuid.New(),TaskID:uuid.New(),RunID:uuid.New(),
  ArtifactID:scope.ExportArtifactID,SHA256:scope.ExportSHA,Bucket:"photos",
  ObjectKey:"tenants/object",ObjectVersion:"version-1",Bytes:10,MediaType:"image/jpeg"}
 raw,err:=json.Marshal(evidence);if err!=nil{t.Fatal(err)}
 ready,err:=readyFromDelegatedEvidence(scope,raw,"photos")
 if err!=nil||ready.Ref.ArtifactID!=scope.ExportArtifactID||
  ready.Object.VersionID!="version-1"{t.Fatal("scoped export rejected",err)}
 evidence.ObjectVersion="null";raw,_=json.Marshal(evidence)
 if _,err:=readyFromDelegatedEvidence(scope,raw,"photos");
  !errors.Is(err,v2input.ErrInvalid){t.Fatal("unversioned export accepted",err)}
 evidence.ObjectVersion="version-1";evidence.ArtifactID=uuid.New();raw,_=json.Marshal(evidence)
 if _,err:=readyFromDelegatedEvidence(scope,raw,"photos");
  !errors.Is(err,v2input.ErrInvalid){t.Fatal("foreign export artifact accepted",err)}
}
