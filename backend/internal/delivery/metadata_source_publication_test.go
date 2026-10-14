package delivery

import (
	"bytes"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func TestMetadataSourcePublicationOwnedAndExternallyBound(t *testing.T) {
	author := "Artist"
	raw, err := canonical.Marshal(MetadataDocument{Author: &author})
	if err != nil {
		t.Fatal(err)
	}
	scope := MetadataSourcePublicationScope{Tenant: uuid.New(), Project: uuid.New(), Actor: uuid.New(), Session: uuid.New(),
		Binding:  v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("b", 64)}, CanvasHash: strings.Repeat("c", 64)},
		Original: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("d", 64)}, OriginalBytes: 100, OriginalMIME: "image/jpeg"}
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: digest(raw)}
	approved := []string{"author"}
	plan, err := CompileMetadataSourcePublication(scope, ref, raw, "approved_exif", approved)
	if err != nil {
		t.Fatal(err)
	}
	body := plan.JCS()
	pin := plan.SHA256()
	if _, err = RestoreMetadataSourcePublication(body, pin, scope, ref, raw, "approved_exif", approved); err != nil {
		t.Fatal(err)
	}
	approved[0] = "gps"
	raw[0] = '!'
	exported := plan.JCS()
	exported[0] = '!'
	exported = plan.SourceJCS()
	exported[0] = '!'
	exported = plan.FrozenJCS()
	exported[0] = '!'
	if !bytes.Equal(body, plan.JCS()) || digest(plan.SourceJCS()) != ref.SHA256 || plan.FrozenJCS()[0] != '{' {
		t.Fatal("mutable publication")
	}
	source := plan.SourceJCS()
	for _, mode := range []string{"tenant", "project", "actor", "session", "version", "original", "bytes", "mime", "approval", "policy", "body", "pin"} {
		t.Run(mode, func(t *testing.T) {
			current := scope
			policy := "approved_exif"
			fields := []string{"author"}
			candidate := append([]byte(nil), body...)
			hash := pin
			switch mode {
			case "tenant":
				current.Tenant = uuid.New()
			case "project":
				current.Project = uuid.New()
			case "actor":
				current.Actor = uuid.New()
			case "session":
				current.Session = uuid.New()
			case "version":
				current.Binding.AssetVersionID = uuid.New()
			case "original":
				current.Original.ArtifactID = uuid.New()
			case "bytes":
				current.OriginalBytes++
			case "mime":
				current.OriginalMIME = "image/png"
			case "approval":
				fields = []string{"gps"}
			case "policy":
				policy = "copyright"
				fields = nil
			case "body":
				candidate[0] = '!'
				hash = digest(candidate)
			case "pin":
				hash = strings.Repeat("e", 64)
			}
			if _, err := RestoreMetadataSourcePublication(candidate, hash, current, ref, source, policy, fields); err == nil {
				t.Fatal("substitution accepted")
			}
		})
	}
	for _, mode := range []string{"alias", "oversized", "unknown_mime", "no_actor"} {
		current := scope
		switch mode {
		case "alias":
			current.Original.ArtifactID = ref.ArtifactID
		case "oversized":
			current.OriginalBytes = 2*1024*1024*1024 + 1
		case "unknown_mime":
			current.OriginalMIME = "image/raw"
		case "no_actor":
			current.Actor = uuid.Nil
		}
		if _, err := CompileMetadataSourcePublication(current, ref, source, "copyright", nil); err == nil {
			t.Fatal(mode)
		}
	}
	for _, media := range []string{"image/x-adobe-dng", "image/x-canon-cr2", "image/x-nikon-nef",
		"image/x-sony-arw", "image/x-fuji-raf", "image/x-olympus-orf", "image/x-panasonic-rw2"} {
		current := scope
		current.OriginalMIME = media
		candidate, err := CompileMetadataSourcePublication(current, ref, source, "copyright", nil)
		if err != nil {
			t.Fatal(media, err)
		}
		if _, err := RestoreMetadataSourcePublication(candidate.JCS(), candidate.SHA256(), current, ref, source, "copyright", nil); err != nil {
			t.Fatal(media, err)
		}
	}
}
