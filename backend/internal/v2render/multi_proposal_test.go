package v2render

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"testing"
)

type multiProposalTestSource struct {
	*multiPipelineSource
	calls  int
	mutate bool
}

func (s *multiProposalTestSource) Binding(_ context.Context, _ auth.Identity, _, _, _ uuid.UUID, _ string) (AdoptedMultiBinding, error) {
	s.calls++
	b := s.binding
	if s.mutate && s.calls > 1 {
		b.ResultID = uuid.New()
	}
	return b, nil
}
func TestMultiProposalDescribeUsesCompleteContentAndCurrentBinding(t *testing.T) {
	for _, op := range []string{"optical.panorama", "optical.focus_stack"} {
		for _, kind := range []string{"valid", "mask", "stale"} {
			t.Run(op+"/"+kind, func(t *testing.T) {
				raw, objects, task := multiPipelineFixture(t, op, kind)
				source := &multiProposalTestSource{multiPipelineSource: raw, mutate: kind == "stale"}
				service := MultiProposalService{Source: source, Objects: objects, Bucket: "photos"}
				tenant, project := uuid.New(), uuid.New()
				proposal, err := service.Describe(context.Background(), auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, tenant, project, task, op)
				switch kind {
				case "valid":
					if err != nil {
						t.Fatal(err)
					}
					if proposal.Tenant != tenant || proposal.Project != project || proposal.Producer != task || proposal.Operation != op || proposal.Status != "review" || !proposal.RequiresReview || len(proposal.Sources) != 2 || len(proposal.Registrations) != 2 || proposal.Reason == "" {
						t.Fatal("incomplete review candidate")
					}
					if source.calls != 2 || objects.opened != 15 {
						t.Fatalf("missing current proof %d/%d", source.calls, objects.opened)
					}
				case "mask":
					if !errors.Is(err, v2input.ErrInvalid) {
						t.Fatal("corrupt candidate described", err)
					}
				case "stale":
					if !errors.Is(err, v2input.ErrExpired) {
						t.Fatal("stale candidate described", err)
					}
				}
				if objects.opened != objects.closed {
					t.Fatal("stream leak")
				}
			})
		}
	}
}
func TestMultiProposalPreflight(t *testing.T) {
	service := MultiProposalService{}
	if _, err := service.Describe(context.Background(), auth.Identity{}, uuid.Nil, uuid.Nil, uuid.Nil, "optical.panorama"); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.Describe(ctx, auth.Identity{}, uuid.Nil, uuid.Nil, uuid.Nil, ""); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
