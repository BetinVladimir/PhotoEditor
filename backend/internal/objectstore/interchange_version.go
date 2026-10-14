package objectstore

import (
 "unicode"
 "unicode/utf8"
)

// ValidInterchangeVersionID validates a physical archive version receipt; it
// does not establish object existence, ownership or content integrity.
func ValidInterchangeVersionID(version string)bool{
 if version==""||version=="null"||len(version)>1024||!utf8.ValidString(version){return false}
 for _,r:=range version{if unicode.IsControl(r){return false}}
 return true
}
