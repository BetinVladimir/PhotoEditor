package exportpreset

import (
 "strings"
 "unicode"
)

// ValidNamingTemplate admits only flat names and known deterministic tokens.
// Collision resolution and the format extension belong to packaging.
func ValidNamingTemplate(value string) bool {
 if strings.TrimSpace(value)=="" || len(value)>200 || strings.ContainsAny(value,"/\\") { return false }
 for _,r:=range value { if unicode.IsControl(r) {return false} }
 for _,token:=range []string{"{original_name}","{name}","{sha8}","{ordinal}"} {value=strings.ReplaceAll(value,token,"")}
 return !strings.ContainsAny(value,"{}")
}
