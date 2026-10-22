package httpapi

import (
    "net/http"
    "net/http/httptest"
    "testing"

    "photoeditor/backend/internal/domain"
)

// Authored boundary scenarios; execution is deferred with the rest of the MVP suite.
func TestRevisionHeaderRequiresCanonicalQuotedSignedInt64(t *testing.T) {
    for _,allowZero:=range []bool{false,true} {
        for _,header:=range []string{`"+1"`,`"01"`,`"00"`,`"-0"`,`"-1"`,`" 1"`,`"1 "`,`W/"1"`,`1`,`"9223372036854775808"`,`"1,2"`,`""`} {
            req:=httptest.NewRequest("PATCH","/",nil)
            req.Header.Set("If-Match",header)
            var err error
            if allowZero { _,err=revisionAllowZero(req) } else { _,err=revision(req) }
            if err==nil || domain.AsError(err).HTTPStatus!=422 { t.Fatalf("zero=%v header=%q error=%v",allowZero,header,err) }
        }
        req:=httptest.NewRequest("PATCH","/",nil)
        var err error
        if allowZero { _,err=revisionAllowZero(req) } else { _,err=revision(req) }
        if err==nil || domain.AsError(err).HTTPStatus!=428 { t.Fatalf("missing header: %v",err) }
    }
    for _,test:=range []struct{header string;value int64}{{`"1"`,1},{`"9007199254740993"`,9007199254740993},{`"9223372036854775807"`,9223372036854775807}} {
        req:=httptest.NewRequest("PATCH","/",nil);req.Header.Set("If-Match",test.header)
        for _,parse:=range []func(*http.Request)(int64,error){revision,revisionAllowZero} {
            got,err:=parse(req);if err!=nil || got!=test.value {t.Fatalf("%s: %d %v",test.header,got,err)}
        }
    }
    req:=httptest.NewRequest("PUT","/",nil);req.Header.Set("If-Match",`"0"`)
    if _,err:=revision(req);err==nil {t.Fatal("positive revision accepted zero")}
    if got,err:=revisionAllowZero(req);err!=nil || got!=0 {t.Fatalf("initial revision: %d %v",got,err)}
}
