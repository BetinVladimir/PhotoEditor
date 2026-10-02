package workflow

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"reflect"
	"testing"
)

type ingestRegistrar struct {
	plan IngestPlan
	err  error
	done bool
}

func (r *ingestRegistrar) Register(context.Context, uuid.UUID, IngestSpec) (IngestPlan, error) {
	r.done = true
	return r.plan, r.err
}

type ingestPublisher struct {
	base      documentPublisher
	registrar *ingestRegistrar
	failAt    int
}

func (p *ingestPublisher) Publish(c context.Context, s artifactjson.Scope, b []byte) (artifactjson.Ref, error) {
	if !p.registrar.done {
		panic("publication preceded registration")
	}
	if len(p.base.calls)+1 == p.failAt {
		return artifactjson.Ref{}, errors.New("storage failure")
	}
	return p.base.Publish(c, s, b)
}
func TestIngestDocumentsRegisterThenPublishAndResume(t *testing.T) {
	spec, registry := ingestFixture(t)
	plan, _ := CompileIngest(spec, registry)
	registrar := &ingestRegistrar{plan: plan}
	publisher := &ingestPublisher{registrar: registrar, failAt: 3}
	builder := IngestDocumentBuilder{Plans: registrar, Publisher: publisher}
	tenant := uuid.New()
	if out, err := builder.Publish(context.Background(), tenant, spec); err == nil || out != nil {
		t.Fatal("partial publication exposed")
	}
	prior := append([]artifactjson.Scope{}, publisher.base.calls...)
	publisher.failAt = 0
	publisher.base.calls = nil
	out, err := builder.Publish(context.Background(), tenant, spec)
	if err != nil || len(out) != 6 {
		t.Fatal("resume failed", err)
	}
	if !reflect.DeepEqual(prior, publisher.base.calls[:2]) {
		t.Fatal("retry changed owners")
	}
	for i, node := range out {
		scope := publisher.base.calls[i]
		if scope.Owner != node.Node.ID || scope.Tenant != tenant || scope.Run != spec.RunID || scope.Purpose != "task_params" {
			t.Fatal("wrong scope")
		}
	}
	registrar.err = errors.New("plan conflict")
	publisher.base.calls = nil
	if _, err = builder.Publish(context.Background(), tenant, spec); err == nil || len(publisher.base.calls) != 0 {
		t.Fatal("conflict published params")
	}
}
