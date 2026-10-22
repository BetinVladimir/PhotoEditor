export function downloadFailureItems(error:unknown):{ordinal:number;code:string}[]{
 const value=error as {code?:unknown;items?:unknown}|null;
 if(!value||!["DOWNLOAD_EXPORT_FAILED","DOWNLOAD_ARCHIVE_FAILED"].includes(String(value.code))||!Array.isArray(value.items)||value.items.length>5000)return [];
 const seen=new Set<number>();
 const result:{ordinal:number;code:string}[]=[];
 for(const item of value.items){
  if(!item||!Number.isInteger(item.ordinal)||item.ordinal<0||item.ordinal>4999||seen.has(item.ordinal)||!["SOURCE_INPUT_INVALID","SOURCE_OUTPUT_UNAVAILABLE","EXPORT_FAILED","EXPORT_CANCELLED","EXPORT_SKIPPED","EXPORT_OUTPUT_UNAVAILABLE","ARCHIVE_OUTPUT_UNAVAILABLE","ARCHIVE_FAILED","ARCHIVE_CANCELLED","ARCHIVE_SKIPPED","ARTIFACT_MISSING","ARTIFACT_HASH_MISMATCH"].includes(item.code))return [];
  seen.add(item.ordinal);result.push({ordinal:item.ordinal,code:item.code});
 }
 return result;
}
