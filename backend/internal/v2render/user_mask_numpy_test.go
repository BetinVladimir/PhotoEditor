package v2render

import (
	"bytes"
	"context"
	"encoding/base64"
	"testing"
)

// Generated with numpy.lib.format.write_array(array([[0,.5,1],[1,.25,0]],
// dtype='<f4'), version=(N,0), allow_pickle=False), for N=1,2,3.
func TestUserMaskNPYNumPyWriterFixtures(t *testing.T) {
	for _, encoded := range []string{
		"k05VTVBZAQB2AHsnZGVzY3InOiAnPGY0JywgJ2ZvcnRyYW5fb3JkZXInOiBGYWxzZSwgJ3NoYXBlJzogKDIsIDMpLCB9ICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgIAoAAAAAAAAAPwAAgD8AAIA/AACAPgAAAAA=",
		"k05VTVBZAgB0AAAAeydkZXNjcic6ICc8ZjQnLCAnZm9ydHJhbl9vcmRlcic6IEZhbHNlLCAnc2hhcGUnOiAoMiwgMyksIH0gICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgIAoAAAAAAAAAPwAAgD8AAIA/AACAPgAAAAA=",
		"k05VTVBZAwB0AAAAeydkZXNjcic6ICc8ZjQnLCAnZm9ydHJhbl9vcmRlcic6IEZhbHNlLCAnc2hhcGUnOiAoMiwgMyksIH0gICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgIAoAAAAAAAAAPwAAgD8AAIA/AACAPgAAAAA=",
	} {
		body, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			t.Fatal(err)
		}
		if err := VerifyUserMaskNPY(context.Background(), bytes.NewReader(body), int64(len(body)), maskSHA(body), 3, 2); err != nil {
			t.Fatal("NumPy version", body[6], err)
		}
	}
}
