package queue

import (
	"github.com/google/uuid"
	"testing"
)

func TestWorkerQueueIdentity(t *testing.T) {
	worker := uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	name, err := WorkerQueue(worker, "media")
	if err != nil || name != "q.worker.aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa.media" {
		t.Fatal(name, err)
	}
	for _, class := range []string{"mail", "cpu.*", "", "cpu.other"} {
		if _, err = WorkerQueue(worker, class); err == nil {
			t.Fatal("unscoped class accepted")
		}
	}
	if _, err = WorkerQueue(uuid.Nil, "cpu"); err == nil {
		t.Fatal("nil worker accepted")
	}
}
