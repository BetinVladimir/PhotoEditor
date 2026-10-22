/** Go time.Time JSON is RFC3339/RFC3339Nano with an explicit UTC offset. */
export function semanticWireTime(value:unknown):value is string{
 return typeof value==='string'&&/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?(?:Z|[+-]\d{2}:\d{2})$/.test(value)&&Number.isFinite(Date.parse(value));
}
