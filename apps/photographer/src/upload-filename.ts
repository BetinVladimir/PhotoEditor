// Match Go strings.TrimSpace used by owner upload admission. JS trim also removes
// U+FEFF and omits U+0085, which would produce different persisted identities.
export function ownerUploadFilename(name:string):string {
 return name.replace(/^[\u0009-\u000d\u0020\u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]+|[\u0009-\u000d\u0020\u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]+$/g,'');
}
