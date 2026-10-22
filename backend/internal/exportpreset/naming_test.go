package exportpreset
import "testing"
func TestNamingTemplateKnownFlatTokens(t *testing.T){
 for _,v:=range []string{"{original_name}_edited","print_{ordinal}_{sha8}","portrait"}{if !ValidNamingTemplate(v){t.Fatal(v)}}
 for _,v:=range []string{"","../{original_name}","a\\b","{unknown}","{ordinal","a\nfile"}{if ValidNamingTemplate(v){t.Fatal(v)}}
}
