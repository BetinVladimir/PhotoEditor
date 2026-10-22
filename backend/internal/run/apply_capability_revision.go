package run
import("strconv";"photoeditor/backend/internal/domain")
func parseApplyCapabilityRevision(value string)(int64,error){n,e:=strconv.ParseInt(value,10,64);if e!=nil||n<1||n>9007199254740991||strconv.FormatInt(n,10)!=value{return 0,domain.NewError("VALIDATION_FAILED",422)};return n,nil}
