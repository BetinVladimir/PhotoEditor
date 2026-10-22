package collection

// Keep the public Item.position as the manual organization position. The
// private ordinal is used only for stable server-side effective-sort cursors.
type itemOrderScanner struct { scanner; ordinal *int64 }
func(s itemOrderScanner)Scan(values ...any)error{return s.scanner.Scan(append(values,s.ordinal)...)}
