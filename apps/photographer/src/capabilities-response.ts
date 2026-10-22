import type {Capabilities} from './catalog-types';

const record=(value:unknown):value is Record<string,unknown>=>!!value&&typeof value==='object'&&!Array.isArray(value);
const exact=(value:unknown,keys:string[]):value is Record<string,unknown>=>record(value)&&Object.keys(value).length===keys.length&&keys.every(key=>Object.hasOwn(value,key));
const text=(value:unknown):value is string=>typeof value==='string'&&value.length>0&&value.length<=500;
function backends(value:unknown):string[]{
 if(!Array.isArray(value)||value.length>32||value.some(item=>!text(item)||!['cpu','cuda','mps'].includes(item))||new Set(value).size!==value.length)throw new Error('CAPABILITIES_INVALID');
 return [...value];
}
/** Own the deployment snapshot before it can enable an editor action. */
export function readCapabilities(value:unknown):Capabilities{
 if(!exact(value,['operations'])||!Array.isArray(value.operations)||value.operations.length>4096)throw new Error('CAPABILITIES_INVALID');
 const seen=new Set<string>();
 return {operations:value.operations.map(item=>{
  if(!exact(item,['operation','resource_class','model_id','supported_backends','available_backends','available','status','reason'])||!text(item.operation)||seen.has(item.operation)||!text(item.resource_class)||typeof item.available!=='boolean'||!['ready','unqualified','unsupported'].includes(item.status as string)||item.model_id!==null&&!text(item.model_id)||item.reason!==null&&!text(item.reason))throw new Error('CAPABILITIES_INVALID');
  seen.add(item.operation);
  const supported=backends(item.supported_backends),available=backends(item.available_backends);
  if(item.available!==(item.status==='ready')||item.available&&(available.length===0||available.some(backend=>!supported.includes(backend))||item.reason!==null)||!item.available&&(available.length!==0||item.reason===null)||item.status==='unsupported'&&supported.length!==0)throw new Error('CAPABILITIES_INVALID');
  return {operation:item.operation,resource_class:item.resource_class,supported_backends:supported,available_backends:available,available:item.available,status:item.status as 'ready'|'unqualified'|'unsupported',...(item.model_id!=null?{model_id:item.model_id as string}:{}),...(item.reason!=null?{reason:item.reason as string}:{})};
 })};
}
