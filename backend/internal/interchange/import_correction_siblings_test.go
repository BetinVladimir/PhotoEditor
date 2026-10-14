package interchange
import (
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)
func TestCorrectionSiblingSourceRequiresUniqueAdoptedEntry(t *testing.T){
 for _,change:=range []string{"valid","ambiguous","not_adopted","foreign_sha","scope","missing_master","missing_version","count"}{t.Run(change,func(t *testing.T){
  parsed:=importParsedValidatorFixture()
  first:=parsed.Manifest.Entries[0];second:=first;second.ID=uuid.New();second.ImageSHA=rawSHA([]byte("second"))
  parsed.Manifest.Entries=append(parsed.Manifest.Entries[:0],first,second)
  versionA,versionB:=uuid.New(),uuid.New()
  sources:=ImportSourceInventory{document:importSourcesDocument{Entries:[]importSourceEntry{{ID:first.ID,State:"adopted",Version:&versionA,Master:&v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("masterA"))}},{ID:second.ID,State:"adopted",Version:&versionB,Master:&v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("masterB"))}}}}}
  sha:=second.ImageSHA
  switch change{case "ambiguous":parsed.Manifest.Entries[0].ImageSHA=sha;case "not_adopted":sources.document.Entries[1].State="rejected";case "foreign_sha":sha=rawSHA([]byte("foreign"));case "scope":sources.document.Entries[1].ID=uuid.New();case "missing_master":sources.document.Entries[1].Master=nil;case "missing_version":sources.document.Entries[1].Version=nil;case "count":sources.document.Entries=sources.document.Entries[:1]}
  index,err:=uniqueImportCorrectionSource(parsed,sources,sha)
  if change=="valid"{if err!=nil||index!=1{t.Fatalf("unique source %d %v",index,err)}}else if err==nil{t.Fatal("unqualified sibling accepted")}
 })}
}
