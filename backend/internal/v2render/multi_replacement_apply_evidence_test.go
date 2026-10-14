package v2render

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"testing"
 "strings"
 "photoeditor/backend/internal/v2input"
)

func TestMultiReplacementApplyEvidenceCompleteOwnedAndScoped(t *testing.T) {
	for _, kind := range []string{"planar", "focus", "cylindrical"} {
		t.Run(kind, func(t *testing.T) {
			operation, mode := "optical.panorama", "valid"
			if kind == "focus" {
				operation = "optical.focus_stack"
			}
			if kind == "cylindrical" {
				mode = "cylindrical"
			}
			s, o, _ := multiPipelineFixture(t, operation, mode)
			who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
			tenant, project := uuid.New(), uuid.New()
            identity:=MultiReplacementBindingIdentity{Tenant:tenant,Project:project,Review:uuid.New(),DecisionHash:strings.Repeat("d",64),OriginalRun:uuid.New(),OriginalTask:uuid.New()}
            namespace:=uuid.NewSHA1(tenant,[]byte("V29.NegativeReplacement/v2:"+project.String()+":"+identity.Review.String()+":"+identity.DecisionHash))
            s.binding.Run=uuid.NewSHA1(namespace,[]byte("run:"+identity.OriginalRun.String()+":"+identity.OriginalTask.String()))
            s.binding.Workflow,_=v2input.WorkflowID(s.binding.Run);s.binding.Replacement=identity
            s.multiOriginalInventoryTestSource.inventory.binding=s.binding
            task:=uuid.NewSHA1(namespace,[]byte("task:"+identity.OriginalTask.String()))

			content, err := LoadAdoptedMultiContent(context.Background(), s, o, who, tenant, project, task, operation, s.binding.Result, "photos")
			if err != nil {
				t.Fatal(err)
			}
			originals, err := content.originalInventory(s.binding, task)
			if err != nil || len(originals) < 2 {
				t.Fatal("missing original inventory", err)
			}
			savedOriginals := string(originals)
			originals[0] = 'x'
			again, err := content.originalInventory(s.binding, task)
			if err != nil || string(again) != savedOriginals {
				t.Fatal("original inventory aliases caller", err)
			}
			if _, err := content.originalInventory(s.binding, uuid.New()); err == nil {
				t.Fatal("foreign original producer accepted")
			}
			root, image, raw, err := content.applyEvidence(tenant, project, task, s.binding)
			if err != nil {
				t.Fatal(err)
			}
			var witnesses []MultiPreviewObjectWitness
			if json.Unmarshal(raw, &witnesses) != nil || len(witnesses) != len(content.cataloguePins)+1 {
				t.Fatal("incomplete witnesses")
			}
			if witnesses[0].Ref != s.binding.Result || len(root) < 2 || len(image) < 2 {
				t.Fatal("missing root/image")
			}
			savedRoot, savedImage, savedWitness := string(root), string(image), string(raw)
			root[0] = 'x'
			image[0] = 'x'
			raw[0] = 'x'
			r, i, w, err := content.applyEvidence(tenant, project, task, s.binding)
			if err != nil || string(r) != savedRoot || string(i) != savedImage || string(w) != savedWitness {
				t.Fatal("aliased evidence", err)
			}
			if _, _, _, err := content.applyEvidence(tenant, uuid.New(), task, s.binding); err == nil {
				t.Fatal("foreign project accepted")
			}
			if _, _, _, err := content.applyEvidence(tenant, project, uuid.New(), s.binding); err == nil {
				t.Fatal("foreign producer accepted")
			}
			changed:=s.binding;changed.Replacement.Review=uuid.New()
            if _,_,_,err:=content.applyEvidence(tenant,project,task,changed);err==nil{t.Fatal("foreign replacement review accepted")}
            changed=s.binding;changed.Replacement=MultiReplacementBindingIdentity{}
            if _,_,_,err:=content.applyEvidence(tenant,project,task,changed);err==nil{t.Fatal("replacement identity stripped")}
            content.cataloguePins = nil
			if _, _, _, err := content.applyEvidence(tenant, project, task, s.binding); err == nil {
				t.Fatal("missing output capture accepted")
			}
		})
	}
	if _, _, _, err := (MultiResultContent{}).applyEvidence(uuid.New(), uuid.New(), uuid.New(), AdoptedMultiBinding{}); err == nil {
		t.Fatal("zero proof accepted")
	}
}

func TestMultiReplacementContentRejectsCorruptPhysicalOutputs(t *testing.T){
 for _,operation:=range []string{"optical.panorama","optical.focus_stack"}{
  for _,bad:=range []string{"shape","alpha","mask"}{t.Run(operation+"/"+bad,func(t *testing.T){
   source,objects,_:=multiPipelineFixture(t,operation,bad)
   tenant,project:=uuid.New(),uuid.New()
   identity:=MultiReplacementBindingIdentity{Tenant:tenant,Project:project,Review:uuid.New(),DecisionHash:strings.Repeat("d",64),OriginalRun:uuid.New(),OriginalTask:uuid.New()}
   namespace:=uuid.NewSHA1(tenant,[]byte("V29.NegativeReplacement/v2:"+project.String()+":"+identity.Review.String()+":"+identity.DecisionHash))
   source.binding.Run=uuid.NewSHA1(namespace,[]byte("run:"+identity.OriginalRun.String()+":"+identity.OriginalTask.String()))
   source.binding.Workflow,_=v2input.WorkflowID(source.binding.Run);source.binding.Replacement=identity
   source.multiOriginalInventoryTestSource.inventory.binding=source.binding
   task:=uuid.NewSHA1(namespace,[]byte("task:"+identity.OriginalTask.String()))
   _,err:=LoadAdoptedMultiContent(context.Background(),source,objects,auth.Identity{UserID:uuid.New(),SessionID:uuid.New()},tenant,project,task,operation,source.binding.Result,"photos")
   if err==nil{t.Fatal("corrupt replacement output accepted")}
   if objects.opened!=objects.closed{t.Fatal("replacement stream leaked")}
  })}
 }
}
