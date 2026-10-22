import type {ApiClient} from '@photoeditor/api-client';
import {readOrganizationSection,type OrganizationSection} from './collection-organization-api';
import type {SectionCreateRequest} from './section-create-attempt-store';
export type SectionCreationResolution={status:'abandoned'}|{status:'created';section:OrganizationSection};
export async function abandonSectionCreation(client:Pick<ApiClient,'request'>,tenant:string,collection:string,request:SectionCreateRequest,key:string,signal:AbortSignal):Promise<SectionCreationResolution>{
 const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
 if(!uuid(tenant)||!uuid(collection)||typeof key!=='string'||!/^[!-~]{1,128}$/.test(key)||!request||Object.keys(request).sort().join(',')!=='expected_revision,name,position'||typeof request.name!=='string'||!request.name.trim()||Array.from(request.name).length>120||!Number.isSafeInteger(request.position)||request.position<0||!Number.isSafeInteger(request.expected_revision)||request.expected_revision<1)throw new Error('SECTION_ABANDON_INVALID');
 const frozen:SectionCreateRequest={name:request.name,position:request.position,expected_revision:request.expected_revision};
 signal.throwIfAborted();const raw=await client.request<unknown>(`/api/v1/tenants/${encodeURIComponent(tenant)}/collections/${encodeURIComponent(collection)}/sections/abandon`,{method:'POST',body:frozen,idempotencyKey:key,signal});signal.throwIfAborted();
 if(raw&&typeof raw==='object'&&!Array.isArray(raw)&&'status' in raw){const v=raw as Record<string,unknown>;if(Object.keys(v).sort().join(',')!=='collection_id,key,status'||v.status!=='abandoned'||v.key!==key||v.collection_id!==collection)throw new Error('SECTION_ABANDON_INVALID');return {status:'abandoned'};}
 const section=readOrganizationSection(raw,collection);if(section.name!==frozen.name.trim()||section.position!==frozen.position)throw new Error('SECTION_ABANDON_INVALID');return {status:'created',section};
}
