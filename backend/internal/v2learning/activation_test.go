package v2learning

import (
	"encoding/json"
	"testing"
)

func TestActivationRevisionAndReviewRequest(t *testing.T) {
	for _, value := range []string{"", "0", "01", "-1", "9007199254740992", "9223372036854775808"} {
		if _, valid := positiveRevision(value); valid {
			t.Fatalf("accepted invalid revision %q", value)
		}
	}
	if revision, valid := positiveRevision("9007199254740991"); !valid || revision != 9007199254740991 {
		t.Fatal("rejected maximum safe revision")
	}
	for _, body := range []string{
		`{"expected_revision":"1","evaluation_sha":"a","reason":"checked"}`,
		`{"expected_revision":"1","evaluation_sha":"a","accept":null,"reason":"checked"}`,
		`{"expected_revision":"1","evaluation_sha":"a","accept":true,"reason":"checked","extra":1}`,
	} {
		var request LearningSafetyReviewRequest
		if err := json.Unmarshal([]byte(body), &request); err == nil {
			t.Fatalf("accepted invalid review request %s", body)
		}
	}
	var request LearningSafetyReviewRequest
	if err := json.Unmarshal([]byte(`{"expected_revision":"1","evaluation_sha":"a","accept":false,"reason":"checked"}`), &request); err != nil || request.Accept {
		t.Fatalf("rejected explicit false review: %v", err)
	}
}
