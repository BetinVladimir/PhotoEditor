package semanticproposal

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestNativeReadOperationSharesSiblingsButFencesBeforeReturn(t *testing.T) {
	for _, revoked := range []bool{false, true} {
		ctx := context.Background()
		who := phaseTestIdentity()
		color := TransferColorProfile{}
		builds, checks := 0, 0
		live := true
		var retained SQLTransferSourceReader
		leaf := func(r SQLTransferSourceReader) (phaseProof, error) {
			return nativeRead(ctx, r, who, color, nativeReadKey{"detector", "sealed"}, clonePhaseProof, func(SQLTransferSourceReader) (phaseProof, error) { builds++; return phaseProof{[]int{7}}, nil }, func(SQLTransferSourceReader, phaseProof) func(context.Context) error {
				return func(context.Context) error {
					checks++
					if !live {
						return errors.New("revoked")
					}
					return nil
				}
			})
		}
		err := nativeReadOperation(ctx, SQLTransferSourceReader{}, who, color, func(r SQLTransferSourceReader) error {
			retained = r
			for i := 0; i < 4; i++ {
				v, err := leaf(r)
				if err != nil {
					return err
				}
				if v.values[0] != 7 {
					t.Fatal("mutable alias")
				}
				v.values[0] = 99
			}
			live = !revoked
			return nil
		})
		if (err != nil) != revoked || builds != 1 || checks != 1 {
			t.Fatalf("revoked=%v builds=%d checks=%d err=%v", revoked, builds, checks, err)
		}
		if _, err := leaf(retained); err == nil {
			t.Fatal("phase escaped")
		}
		live = true
		if _, err := leaf(SQLTransferSourceReader{}); err != nil || builds != 2 || checks != 2 {
			t.Fatal("fresh read reused closed scope", err, builds, checks)
		}
	}
}

func TestNativeReadOperationNestedFailureCannotBeSwallowed(t *testing.T) {
	ctx := context.Background()
	who := phaseTestIdentity()
	sentinel := errors.New("physical refusal")
	err := nativeReadOperation(ctx, SQLTransferSourceReader{}, who, TransferColorProfile{}, func(r SQLTransferSourceReader) error {
		_ = nativeReadOperation(ctx, r, who, TransferColorProfile{}, func(SQLTransferSourceReader) error { return sentinel })
		return nil
	})
	if !errors.Is(err, sentinel) {
		t.Fatal("nested failure discarded", err)
	}
}

func TestNativeReadOperationCancellationAndForeignScope(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	err := nativeReadOperation(ctx, SQLTransferSourceReader{}, auth.Identity{}, TransferColorProfile{}, func(SQLTransferSourceReader) error { called = true; return nil })
	if !errors.Is(err, context.Canceled) || called {
		t.Fatal("cancelled callback ran", err)
	}
	who := phaseTestIdentity()
	ctx = context.Background()
	err = nativeReadOperation(ctx, SQLTransferSourceReader{}, who, TransferColorProfile{}, func(r SQLTransferSourceReader) error {
		other := who
		other.Epoch++
		return nativeReadOperation(ctx, r, other, TransferColorProfile{}, func(SQLTransferSourceReader) error { t.Fatal("foreign callback ran"); return nil })
	})
	if err == nil {
		t.Fatal("foreign scope accepted")
	}
}

// The wider operation must hash-check unchanged locators again, even when its
// sibling completed reads have successfully reused immutable typed results.
func TestNativeReadOperationRejectsSameLocatorPhysicalMutation(t *testing.T) {
	ctx := context.Background()
	who := phaseTestIdentity()
	objects := &phaseMemoryObjects{body: "{}"}
	obj := objectstore.Object{Key: "original/key", VersionID: "v1", ContentType: "application/json", Bytes: 2, SHA256: "44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a"}
	err := nativeReadOperation(ctx, SQLTransferSourceReader{Objects: objects}, who, TransferColorProfile{}, func(r SQLTransferSourceReader) error {
		_, err := v2input.ReadPinnedSemanticDocument(ctx, r.Objects, obj, v2input.Ref{ArtifactID: uuid.New(), SHA256: obj.SHA256})
		if err != nil {
			return err
		}
		objects.body = "[]"
		return nil
	})
	if err == nil || objects.opens != 2 {
		t.Fatalf("changed bytes accepted err=%v opens=%d", err, objects.opens)
	}
}
