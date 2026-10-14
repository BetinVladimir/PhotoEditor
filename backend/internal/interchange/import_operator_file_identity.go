package interchange

import (
	"os"
	"syscall"
)

// A process-owned operator declaration must have one filesystem name. Check
// each stat snapshot so a link added while bytes are read is also rejected.
func singleLinkImportOperatorFile(info os.FileInfo) bool {
	if info == nil {
		return false
	}
	identity, ok := info.Sys().(*syscall.Stat_t)
	return ok && identity.Nlink == 1
}
