package semanticreview

import (
 "testing"
 "github.com/google/uuid"
)

func TestRemovalIssuedLinkageRejectsChangedVerifiedProducer(t *testing.T) {
 d:=buildRemovalPhysicalDocuments(t)
 expected:=RemovalPreviewPresentationLinkage{Document:PreviewPresentationDocument{Source:PlanSource{Ref:d.presentationRef,Version:"immutable-v1"},TaskID:uuid.New(),ResultID:uuid.New(),Attempt:1,Fence:1}}
 for _,mode:=range []string{"valid","task","result","attempt","fence","document_ref","document_version","sidecar_ref","sidecar_version","sidecar_bytes"} {
  t.Run(mode,func(t *testing.T) {
   current:=expected
   switch mode {
   case "task":current.Document.TaskID=uuid.New()
   case "result":current.Document.ResultID=uuid.New()
   case "attempt":current.Document.Attempt++
   case "fence":current.Document.Fence++
   case "document_ref":current.Document.Source.Ref.ArtifactID=uuid.New()
   case "document_version":current.Document.Source.Version="replacement-v2"
   case "sidecar_ref":current.Sidecars[3].Ref.ArtifactID=uuid.New()
   case "sidecar_version":current.Sidecars[3].Version="replacement-v2"
   case "sidecar_bytes":current.Sidecars[3].Bytes++
   }
   err:=checkRemovalIssuedLinkage(expected,current)
   if (err==nil)!=(mode=="valid") {t.Fatal(mode,err)}
  })
 }
}
