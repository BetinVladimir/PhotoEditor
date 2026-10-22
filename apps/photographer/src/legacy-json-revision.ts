/** Legacy handlers accept JSON integer revisions, unlike V2 decimal-string
 * contracts. Never round a CAS token during conversion to a JSON number. */
export function legacyJSONRevision(value:unknown):number{
 if(typeof value!=='string'||!/^(0|[1-9][0-9]{0,18})$/.test(value)||BigInt(value)>BigInt(Number.MAX_SAFE_INTEGER))throw new Error('LEGACY_REVISION_NOT_EXACT');
 return Number(value);
}
