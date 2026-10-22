import type {ApiClient} from '@photoeditor/api-client';
import type {SectionCreateRequest} from './section-create-attempt-store';
export type OrganizationSection={id:string;collection_id:string;name:string;position:number;revision:string;created_at:string};
export type OrganizationItem={id:string;collection_id:string;asset_id:string;asset_version_id:string;section_id?:string;position:number};
export type OrganizationState={revision:string;state:string;sections:OrganizationSection[];items:OrganizationItem[]};
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
const obj=(v:unknown):v is Record<string,unknown>=>!!v&&typeof v==='object'&&!Array.isArray(v);
const rev=(v:unknown):v is string=>typeof v==='string'&&/^[1-9][0-9]{0,18}$/.test(v)&&(v.length<19||v<='9223372036854775807');
const validPosition=(v:unknown):v is number=>Number.isSafeInteger(v)&&Number(v)>=0;
const validName=(v:unknown):v is string=>typeof v==='string'&&!!v.trim()&&Array.from(v).length<=120;
export function organizationPosition(raw:string){if(!/^(0|[1-9][0-9]*)$/.test(raw)||!Number.isSafeInteger(Number(raw)))throw new Error('POSITION_INVALID');return Number(raw);}
export function readOrganizationSection(v:unknown,collection:string):OrganizationSection{
 if(!obj(v)||Object.keys(v).sort().join(',')!=='collection_id,created_at,id,name,position,revision'||!uuid(v.id)||v.collection_id!==collection||typeof v.name!=='string'||!v.name.trim()||Array.from(v.name).length>120||!Number.isSafeInteger(v.position)||Number(v.position)<0||!rev(v.revision)||typeof v.created_at!=='string'||!Number.isFinite(Date.parse(v.created_at)))throw new Error('SECTION_INVALID');return {...v} as OrganizationSection;
}
function readItem(v:unknown,collection:string):OrganizationItem{
 if(!obj(v)||!uuid(v.id)||v.collection_id!==collection||!uuid(v.asset_id)||!uuid(v.asset_version_id)||v.section_id!==undefined&&!uuid(v.section_id)||!Number.isSafeInteger(v.position)||Number(v.position)<0)throw new Error('ITEM_INVALID');return {id:v.id,collection_id:collection,asset_id:v.asset_id,asset_version_id:v.asset_version_id,...(v.section_id?{section_id:v.section_id as string}:{}),position:v.position as number};
}
export class CollectionOrganizationAPI{
 private root:string;
 constructor(private client:Pick<ApiClient,'request'>,tenant:string,private collection:string){if(!uuid(tenant)||!uuid(collection))throw new Error('SCOPE_INVALID');this.root=`/api/v1/tenants/${tenant}/collections/${collection}`;}
 async load(signal:AbortSignal):Promise<OrganizationState>{
  signal.throwIfAborted();const c=await this.client.request<unknown>(this.root,{signal});signal.throwIfAborted();if(!obj(c)||c.id!==this.collection||!rev(c.revision)||typeof c.state!=='string'||!['draft','published','archived','deleting'].includes(c.state))throw new Error('COLLECTION_INVALID');
  const revision=c.revision;
  const pages=async<T,>(suffix:string,decode:(v:unknown)=>T):Promise<T[]>=>{const all:T[]=[],ids=new Set<string>(),seen=new Set<string>();let cursor:string|undefined;
   do{signal.throwIfAborted();const p=await this.client.request<unknown>(`${this.root}/${suffix}?limit=100${cursor?'&cursor='+encodeURIComponent(cursor):''}`,{signal});signal.throwIfAborted();if(!obj(p)||p.revision!==revision||!Array.isArray(p.items)||p.items.length>100)throw new Error('COLLECTION_CHANGED');for(const raw of p.items){const v=decode(raw);const id=(v as {id:string}).id;if(ids.has(id)||all.length>=5000)throw new Error('PAGE_INVALID');ids.add(id);all.push(v);}const next=p.next_cursor;if(next!==null&&next!==undefined&&(typeof next!=='string'||!next||seen.has(next)||!p.items.length))throw new Error('CURSOR_INVALID');cursor=next as string|undefined;if(cursor)seen.add(cursor);
   }while(cursor);return all;};
  const sections=await pages('sections',v=>readOrganizationSection(v,this.collection)),items=await pages('items',v=>readItem(v,this.collection));
  // Recheck the revision after all pages, including the empty final section page.
  const last=await this.client.request<unknown>(this.root,{signal});signal.throwIfAborted();if(!obj(last)||last.revision!==revision||last.id!==this.collection)throw new Error('COLLECTION_CHANGED');
  const sectionIDs=new Set(sections.map(x=>x.id));if(items.some(x=>x.section_id&&!sectionIDs.has(x.section_id)))throw new Error('SECTION_INVALID');return {revision,state:c.state,sections,items};
 }
 async create(request:SectionCreateRequest,key:string,signal:AbortSignal){signal.throwIfAborted();if(!validName(request.name)||!validPosition(request.position)||!Number.isSafeInteger(request.expected_revision)||request.expected_revision<1||!uuid(key))throw new Error('SECTION_INVALID');const frozen={name:request.name,position:request.position,expected_revision:request.expected_revision};const v=await this.client.request<unknown>(this.root+'/sections',{method:'POST',body:frozen,idempotencyKey:key,signal});signal.throwIfAborted();const section=readOrganizationSection(v,this.collection);if(section.name!==frozen.name.trim()||section.position!==frozen.position)throw new Error('SECTION_RECEIPT_INVALID');return section;}
 async update(section:string,revision:string,name:string,position:number,signal:AbortSignal){signal.throwIfAborted();if(!uuid(section)||!rev(revision)||!validName(name)||!validPosition(position))throw new Error('SECTION_INVALID');const v=await this.client.request<unknown>(`${this.root}/sections/${section}`,{method:'PATCH',headers:{'If-Match':`"${revision}"`},body:{name,position},signal});signal.throwIfAborted();const out=readOrganizationSection(v,this.collection);if(out.id!==section||out.name!==name.trim()||out.position!==position)throw new Error('SECTION_RECEIPT_INVALID');return out;}
 async remove(section:string,revision:string,signal:AbortSignal){signal.throwIfAborted();if(!uuid(section)||!rev(revision))throw new Error('SECTION_INVALID');await this.client.request(`${this.root}/sections/${section}`,{method:'DELETE',headers:{'If-Match':`"${revision}"`},signal});signal.throwIfAborted();}
 async move(item:OrganizationItem,revision:string,position:number,section:string|null,signal:AbortSignal){signal.throwIfAborted();const frozen=readItem(item,this.collection);if(!rev(revision)||!validPosition(position)||section!==null&&!uuid(section))throw new Error('ITEM_INVALID');const v=await this.client.request<unknown>(`${this.root}/items/${frozen.id}`,{method:'PATCH',headers:{'If-Match':`"${revision}"`},body:{position,section_id:section},signal});signal.throwIfAborted();const out=readItem(v,this.collection);if(out.id!==frozen.id||out.asset_id!==frozen.asset_id||out.asset_version_id!==frozen.asset_version_id||out.position!==position||(out.section_id??null)!==section)throw new Error('ITEM_RECEIPT_INVALID');return out;}
}
