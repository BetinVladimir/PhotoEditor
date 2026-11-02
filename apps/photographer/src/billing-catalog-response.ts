const fields=(value:unknown,keys:string[]):value is Record<string,unknown>=>!!value&&typeof value==='object'&&!Array.isArray(value)&&Object.keys(value).sort().join(',')===[...keys].sort().join(',');
const uuid=(value:unknown):value is string=>typeof value==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(value)&&value!=='00000000-0000-0000-0000-000000000000';
function quantity(value:unknown):string{if(typeof value!=='string'||!/^(0|[1-9][0-9]{0,18})$/.test(value)||BigInt(value)>9223372036854775807n)throw new Error('BILLING_CATALOG_INVALID');return value;}
export type CatalogMoney={currency:'EUR';net_minor:string;tax_minor:string;gross_minor:string;tax_mode:'test_bg20'|'approved';tax_lines:Array<{code:string;rate_decimal:string;amount_minor:string}>};
export function readCatalogMoney(raw:unknown):CatalogMoney{
 if(!fields(raw,['currency','net_minor','tax_minor','gross_minor','tax_mode','tax_lines'])||raw.currency!=='EUR'||!['test_bg20','approved'].includes(raw.tax_mode as string)||!Array.isArray(raw.tax_lines))throw new Error('BILLING_CATALOG_INVALID');
 const net=quantity(raw.net_minor),tax=quantity(raw.tax_minor),gross=quantity(raw.gross_minor);
 if(BigInt(net)+BigInt(tax)!==BigInt(gross))throw new Error('BILLING_CATALOG_INVALID');
 const lines=raw.tax_lines.map((value:unknown)=>{
  if(!fields(value,['code','rate_decimal','amount_minor'])||typeof value.code!=='string'||!value.code||typeof value.rate_decimal!=='string'||!/^\d+(?:\.\d+)?$/.test(value.rate_decimal))throw new Error('BILLING_CATALOG_INVALID');
  return {code:value.code,rate_decimal:value.rate_decimal,amount_minor:quantity(value.amount_minor)};
 });
 if(lines.reduce((sum,line)=>sum+BigInt(line.amount_minor),0n)!==BigInt(tax))throw new Error('BILLING_CATALOG_INVALID');
 return {currency:'EUR',net_minor:net,tax_minor:tax,gross_minor:gross,tax_mode:raw.tax_mode as CatalogMoney['tax_mode'],tax_lines:lines};
}
export type CatalogPlan={id:string;version_id:string;code:string;label:string;price:CatalogMoney;limits:Array<{resource_code:string;quantity:string;unit:string}>;can_purchase:boolean;unavailable_reason:string|null};
export type BillingCatalog={catalog_revision:string;scope:string;currency:'EUR';plans:CatalogPlan[]};
export function readBillingCatalog(raw:unknown):BillingCatalog{
 if(!fields(raw,['catalog_revision','scope','currency','plans'])||typeof raw.scope!=='string'||!raw.scope||raw.currency!=='EUR'||!Array.isArray(raw.plans))throw new Error('BILLING_CATALOG_INVALID');
 const ids=new Set<string>(),codes=new Set<string>(),versions=new Set<string>();
 const plans=raw.plans.map((value:unknown):CatalogPlan=>{
  if(!fields(value,['id','version_id','code','label','price','limits','can_purchase','unavailable_reason'])||!uuid(value.id)||!uuid(value.version_id)||typeof value.code!=='string'||!value.code||value.code==='custom'||typeof value.label!=='string'||!value.label||typeof value.can_purchase!=='boolean'||value.unavailable_reason!==null&&typeof value.unavailable_reason!=='string'||!Array.isArray(value.limits)||ids.has(value.id)||codes.has(value.code)||versions.has(value.version_id))throw new Error('BILLING_CATALOG_INVALID');
  ids.add(value.id);codes.add(value.code);versions.add(value.version_id);
  const resources=new Set<string>();
  const limits=value.limits.map((limit:unknown)=>{
   if(!fields(limit,['resource_code','quantity','unit'])||typeof limit.resource_code!=='string'||!limit.resource_code||resources.has(limit.resource_code)||typeof limit.unit!=='string'||!limit.unit)throw new Error('BILLING_CATALOG_INVALID');
   resources.add(limit.resource_code);return {resource_code:limit.resource_code,quantity:quantity(limit.quantity),unit:limit.unit};
  });
  if(value.can_purchase&&(!limits.length||value.unavailable_reason!==null||value.code==='studio_pro'))throw new Error('BILLING_CATALOG_INVALID');
  return {id:value.id,version_id:value.version_id,code:value.code,label:value.label,price:readCatalogMoney(value.price),limits,can_purchase:value.can_purchase,unavailable_reason:value.unavailable_reason as string|null};
 });
 return {catalog_revision:quantity(raw.catalog_revision),scope:raw.scope,currency:'EUR',plans};
}
