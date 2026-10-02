package domain

import "testing"

func TestRoleHierarchy(t *testing.T) {
	if !Owner.Allows(Editor) || !Editor.Allows(Viewer) || Viewer.Allows(Editor) || Role("unknown").Allows(Viewer) || Owner.Allows(Role("unknown")) {
		t.Fatal("role hierarchy grants invalid access")
	}
}
func TestDigest(t *testing.T) {
	if _, err := ParseDigest("bad"); err == nil {
		t.Fatal("accepted bad hash")
	}
}
