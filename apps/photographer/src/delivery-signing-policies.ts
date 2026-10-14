export type DeliverySigningPolicyChoice={id:string;sha256:string;revision:string};
const uuid=(value:unknown):value is string=>typeof value==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(value)&&value!=='00000000-0000-0000-0000-000000000000';
const revision=(value:unknown):value is string=>typeof value==='string'&&value.length<=19&&/^[1-9][0-9]*$/.test(value)&&BigInt(value)<=9223372036854775807n;
export function readDeliverySigningPolicies(value:unknown):DeliverySigningPolicyChoice[]{
 if(!value||typeof value!=='object'||Array.isArray(value))throw new Error('DELIVERY_SIGNING_POLICIES_INVALID');
 const page=value as Record<string,unknown>;
 if(Object.keys(page).length!==1||!Array.isArray(page.items)||page.items.length>256)throw new Error('DELIVERY_SIGNING_POLICIES_INVALID');
 const choices:DeliverySigningPolicyChoice[]=[];
 for(const item of page.items){
  if(!item||typeof item!=='object'||Array.isArray(item))throw new Error('DELIVERY_SIGNING_POLICIES_INVALID');
  const row=item as Record<string,unknown>;
  if(Object.keys(row).length!==3||!uuid(row.id)||typeof row.sha256!=='string'||!/^[0-9a-f]{64}$/.test(row.sha256)||!revision(row.revision)||choices.length>0&&choices[choices.length-1].id>=row.id)throw new Error('DELIVERY_SIGNING_POLICIES_INVALID');
  choices.push({id:row.id,sha256:row.sha256,revision:row.revision});
 }
 return choices;
}

export function deliverySigningMIMEs(formats:ReadonlyArray<'jpeg'|'png'|'tiff'>):string[]{
 if(formats.length<1||formats.length>16||formats.some(format=>format!=='jpeg'&&format!=='png'))throw new Error('DELIVERY_SIGNING_FORMAT_UNAVAILABLE');
 const mimes=[...new Set(formats.map(format=>format==='jpeg'?'image/jpeg':'image/png'))].sort();
 return mimes;
}
