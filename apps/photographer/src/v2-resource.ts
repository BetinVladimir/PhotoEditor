/** Decode the documented V2 Resource envelope; domain rows live in data. */
export function v2ResourceData<T>(value:unknown,kind:string,expectedID?:string):T{
 if(!value||typeof value!=='object')throw new Error('Invalid V2 resource envelope');
 const envelope=value as {id?:unknown;revision?:unknown;data?:unknown};
 if(typeof envelope.id!=='string'||typeof envelope.revision!=='string'||!/^([1-9][0-9]*)$/.test(envelope.revision)||!envelope.data||typeof envelope.data!=='object'||Array.isArray(envelope.data))throw new Error('Invalid V2 resource envelope');
 const data=envelope.data as Record<string,unknown>;
 if(data.resource_type!==kind||data.id!==envelope.id||data.revision!==envelope.revision||(expectedID!==undefined&&envelope.id!==expectedID))throw new Error('V2 resource binding mismatch');
 return data as T;
}
