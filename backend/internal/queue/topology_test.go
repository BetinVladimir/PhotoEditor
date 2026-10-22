package queue

import (
	"photoeditor/backend/internal/workflow"
	"strings"
	"testing"
)

func matches(pattern, key string) bool {
	a, b := strings.Split(pattern, "."), strings.Split(key, ".")
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != "*" && a[i] != b[i] {
			return false
		}
	}
	return true
}
func TestEveryOperationHasOneQueue(t *testing.T) {
	registry, err := workflow.LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	for _, op := range registry {
		count := 0
		for _, b := range Bindings {
			if b.Exchange == Commands && matches(b.Key, op.RoutingKey) {
				count++
				if b.Class != op.ResourceClass {
					t.Fatal("wrong class", op.Name)
				}
			}
		}
		if count != 1 {
			t.Fatal("ambiguous/unroutable operation", op.Name, count)
		}
	}
}


func TestDownloadTerminalEventHasOneDurableEventSink(t *testing.T) {
 count:=0
 for _,binding:=range Bindings {
  if binding.Exchange==Events && matches(binding.Key,"download_status") {
   count++
   if binding.Queue!="q.orchestrator.events" || binding.Class!="events" {t.Fatal("unexpected download event sink")}
  }
 }
 if count!=1 {t.Fatal("unroutable or duplicate download terminal event",count)}
}

func TestReviewCreatedHasOneDurableEventSink(t *testing.T) {
 count:=0
 for _,binding:=range Bindings {
  if binding.Exchange==Events && matches(binding.Key,"review.created") {
   count++
   if binding.Queue!="q.orchestrator.events" || binding.Class!="events" {t.Fatal("unexpected review event sink")}
  }
 }
 if count!=1 {t.Fatal("unroutable or duplicate review event",count)}
}
