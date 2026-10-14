package interchange

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

type importReviewProposalStub struct {
	proposal, base v2input.Ref
	object         objectstore.Object
	reads          int
	change         bool
}

func (s *importReviewProposalStub) ResolveImportReviewProposal(context.Context) (v2input.Ref, v2input.Ref, objectstore.Object, error) {
	s.reads++
	if s.change && s.reads == 2 {
		changed := s.object
		changed.VersionID = "replacement"
		return s.proposal, s.base, changed, nil
	}
	return s.proposal, s.base, s.object, nil
}

type importReviewProposalObjects struct{ raw []byte }

func (o importReviewProposalObjects) Open(context.Context, objectstore.Object) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(o.raw)), nil
}

func TestReadImportReviewProposalBindsPhysicalBytesAndRechecksScope(t *testing.T) {
	raw, err := os.ReadFile("../review/testdata/defect-proposal-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		Image v2input.Ref `json:"image"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	source := &importReviewProposalStub{
		proposal: v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA(raw)}, base: body.Image,
		object: objectstore.Object{Key: "prepared/proposal", VersionID: "version-1", SHA256: rawSHA(raw), Bytes: int64(len(raw)), ContentType: "application/json"},
	}
	selected, proposal, base, err := ReadImportReviewProposal(context.Background(), source, importReviewProposalObjects{raw: raw}, "defects")
	if err != nil || !bytes.Equal(selected, raw) || proposal != source.proposal || base != source.base || source.reads != 2 {
		t.Fatal("prepared proposal read was not stable", err)
	}
	source.reads, source.change = 0, true
	if _, _, _, err := ReadImportReviewProposal(context.Background(), source, importReviewProposalObjects{raw: raw}, "defects"); err == nil {
		t.Fatal("replacement during proposal IO was accepted")
	}
	source.reads, source.change = 0, false
	corrupt := append([]byte(nil), raw...)
	corrupt[len(corrupt)-1] ^= 1
	if _, _, _, err := ReadImportReviewProposal(context.Background(), source, importReviewProposalObjects{raw: corrupt}, "defects"); err == nil {
		t.Fatal("changed proposal bytes accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, _, err := ReadImportReviewProposal(ctx, source, importReviewProposalObjects{raw: raw}, "defects"); !errors.Is(err, context.Canceled) {
		t.Fatal("proposal reader lost cancellation", err)
	}
}
