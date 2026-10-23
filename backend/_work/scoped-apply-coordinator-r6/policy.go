package main

import (
	"errors"
	"github.com/google/uuid"
)

var fixedStyle = uuid.MustParse("8d7c2d08-1fca-45fd-a871-5fafeddf0069")

const fixedStyleSHA = "7e1312654fafadf10042ef1682f6b27f11d3bca1e2b56d6c2ed451df05090e69"

var exactItems = []itemBinding{
	{uuid.MustParse("c655ed68-f1cf-4e84-91a7-4b5cbfab4e71"), uuid.MustParse("6abe9572-a18c-4245-baa5-efc5c480b3cf"), uuid.MustParse("3923b79f-6198-4692-ba0c-57888915c1f6"), uuid.MustParse("edadb7a9-87ee-4704-bcf0-59e4c63d37d2"), "9fb18e104a154e5c5007f3546d9e5bdacec268bde76da8ff5e5e4a80336a4b0d"},
	{uuid.MustParse("75e653c1-b941-4fe9-9393-c91b18c82a30"), uuid.MustParse("909a8d58-8023-420c-8e43-9df16ffcb9a5"), uuid.MustParse("13f3e614-1fcd-4bf7-94e2-ae1ad09493a6"), uuid.MustParse("6bb60710-887b-446a-975f-29e81907419b"), "3248c71bdba7b39bd19987f27eabd63eb8b60c235430be68717fd3ea4f248a42"},
	{uuid.MustParse("7fd26502-6495-4dcc-8964-4000a90a587d"), uuid.MustParse("80b38582-6b2a-4482-b69b-8afb2af8a764"), uuid.MustParse("5bb616d8-6c3c-4828-add8-bc76661bbb7d"), uuid.MustParse("4daca89f-b3f9-4c38-a75f-795ca1a3704a"), "ff48e86d91d329585f177e9c370ba065bbbf56eae9e3632f7177d83d8edc1447"},
}

func validateThree(s scope) error {
	if s.Tenant != uuid.MustParse("5dbe7f17-b560-4696-9525-03959e843841") || s.Style != fixedStyle || s.StyleSHA != fixedStyleSHA || len(s.Items) != 3 || len(s.Tasks) > 12 || len(s.Nodes) > 4 {
		return errors.New("closed three-photo style scope")
	}
	seen := map[uuid.UUID]bool{}
	for _, a := range s.Items {
		found := false
		for _, b := range exactItems {
			if a == b {
				found = true
			}
		}
		if !found || seen[a.Item] {
			return errors.New("source substitution/duplicate")
		}
		seen[a.Item] = true
	}
	seen = map[uuid.UUID]bool{}
	for _, t := range s.Tasks {
		if (t.Class != "cpu" && t.Class != "gpu") || t.ID == uuid.Nil || seen[t.ID] || !digestPattern.MatchString(t.ProfileSHA) {
			return errors.New("observed task identity")
		}
		switch t.Operation {
		case "style.resolve", "image.color", "image.local", "quality.check":
		default:
			return errors.New("unapproved operation")
		}
		seen[t.ID] = true
	}
	for i, n := range s.Nodes {
		if n == "" || len(n) > 256 {
			return errors.New("observed node missing")
		}
		for _, p := range s.Nodes[:i] {
			if p == n {
				return errors.New("duplicate node")
			}
		}
	}
	return nil
}
