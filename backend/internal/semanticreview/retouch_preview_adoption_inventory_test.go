package semanticreview

import (
	"github.com/google/uuid"
	"testing"
)

func TestShownRetouchInventoryRefusesChangedCompilerOrMissingDocuments(t *testing.T) {
	for _, mode := range []string{"missing_documents", "task", "params_sha", "params_bytes", "physical_context"} {
		t.Run(mode, func(t *testing.T) {
			child, approved, selected, physical := shownRetouchCompileFixture(t)
			compiled, err := CompileShownRetouchAdoption(child, approved, selected, physical)
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "task":
				compiled.TaskID = uuid.New()
			case "params_sha":
				compiled.ParamsRef.SHA256 = "invalid"
			case "params_bytes":
				compiled.body = append(compiled.body, ' ')
			case "physical_context":
				physical.adapterContext.ArtifactID = uuid.Nil
			}
			closure, err := CompileShownRetouchAdoptionInventory(compiled, approved, physical, nil, nil, ExposureSourceDocuments{})
			if err == nil || closure.InputDigest != "" || closure.Open != nil {
				t.Fatal("incomplete or changed closure accepted", mode, err)
			}
		})
	}
}
