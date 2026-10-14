package interchange

import (
 "context"
 "testing"
 "github.com/google/uuid"
)

func TestSQLImportActivationInstallationRequiresCompleteAuthority(t *testing.T) {
 documents, _ := importMaterializationFixture(t)
 prepared, err := NewSQLImportActivationPreparation(context.Background(), uuid.New(), documents, nil, ImportResolverDependencies{}, nil)
 if err == nil || prepared.Resolver != nil || prepared.Resources != nil { t.Fatal("incomplete activation installation returned authority") }
}

func TestSQLImportActivationInstallationPreservesCancellation(t *testing.T) {
 ctx, cancel := context.WithCancel(context.Background()); cancel()
 prepared, err := NewSQLImportActivationPreparation(ctx, uuid.Nil, ImportPreparedDocuments{}, nil, ImportResolverDependencies{}, nil)
 if err != context.Canceled || prepared.Resolver != nil || prepared.Resources != nil { t.Fatal("cancelled installation returned authority", err) }
}
