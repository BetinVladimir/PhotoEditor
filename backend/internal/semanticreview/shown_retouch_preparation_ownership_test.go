package semanticreview

import (
	"bytes"
	"context"
	"testing"

	"photoeditor/backend/internal/v2input"
)

func TestShownRetouchPreparationOwnsDocumentsBeforePublisherCallback(t *testing.T) {
	for _, document := range []string{"candidate", "intent", "source", "reference", "mask", "protection", "presentation", "evidence", "working_image"} {
		t.Run(document, func(t *testing.T) {
			f := fullShownRetouchContentFixture(t)
			expected, err := shownRetouchDocumentBundle(f.approved, f.physical, f.candidate, f.intent, f.docs)
			if err != nil {
				t.Fatal(err)
			}
			expected = append([]byte(nil), expected...)
			var borrowed []byte
			switch document {
			case "candidate":
				borrowed = f.candidate
			case "intent":
				borrowed = f.intent
			case "source":
				borrowed = f.docs.Source
			case "reference":
				borrowed = f.docs.Reference
			case "mask":
				borrowed = f.docs.Mask
			case "protection":
				borrowed = f.docs.Protection
			case "presentation":
				borrowed = f.physical.linkage.Document.raw
			case "evidence":
				borrowed = f.physical.linkage.Evidence.raw
			case "working_image":
				borrowed = f.physical.image.ImageBytes
			}
			if len(borrowed) == 0 {
				t.Fatal("missing mutation input")
			}
			recorder := &previewPublicationRecorder{}
			checks, factories := 0, 0
			result, err := PrepareShownRetouchWithPublisher(context.Background(), f.child, f.approved,
				f.selected, f.physical, f.candidate, f.intent, f.docs, f.owner, f.limits, f.pin,
				func(FrozenShownRetouchTask) (v2input.Publisher, error) {
					factories++
					// Simulate the caller changing its buffer while publication waits.
					borrowed[0] = '!'
					return recorder, nil
				}, func(context.Context) error { checks++; return nil })
			if err != nil || factories != 1 || checks != 3 {
				t.Fatal("owned preparation failed after caller buffer changed", err, factories, checks)
			}
			if !bytes.Equal(result.documents, expected) || result.compiled.Params().Pixels != f.selected.Receipt.Pixels {
				t.Fatal("caller buffer replaced frozen document or selected pixels")
			}
		})
	}
}
