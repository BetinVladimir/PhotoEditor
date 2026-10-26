// Package archivecontract centralizes the reviewed archive-only byte envelope.
package archivecontract

const (
	MaxEntries            = 5000
	DocumentBytes         = 8 * 1024 * 1024
	CommandBytes          = 1024 * 1024
	ZIPEntryOverheadBytes = 4096
)
