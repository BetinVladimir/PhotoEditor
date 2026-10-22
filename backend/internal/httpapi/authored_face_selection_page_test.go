package httpapi
import "testing"
func TestAuthoredFaceSelectionPageQueryExact(t *testing.T) {
 for _, c := range []struct{raw string; after, limit int}{{"",0,16},{"after_ordinal=0&limit=1",0,1},{"after_ordinal=4998&limit=16",4998,16}} {
  after, limit, err := authoredFaceSelectionPageQuery(c.raw)
  if err != nil || after != c.after || limit != c.limit { t.Fatalf("%q: %d/%d %v",c.raw,after,limit,err) }
 }
}
func TestAuthoredFaceSelectionPageQueryRejectsAmbiguousAndMalformed(t *testing.T) {
 for _, raw := range []string{"limit=1&limit=2","after_ordinal=0&after_ordinal=1","limit=","after_ordinal=","foreign=1","limit=0","limit=17","after_ordinal=-1","after_ordinal=4999","limit=1.5","limit=999999999999999999999999","limit=%ZZ","limit=1;after_ordinal=0"} {
  if _, _, err := authoredFaceSelectionPageQuery(raw); err == nil { t.Fatalf("accepted %q",raw) }
 }
}
