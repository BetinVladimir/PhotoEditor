import type {Member,Page} from './catalog-types';
const uuid=(value:unknown):value is string=>typeof value==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(value)&&value!=='00000000-0000-0000-0000-000000000000';
const revision=(value:unknown)=>typeof value==='string'&&/^[1-9][0-9]{0,18}$/.test(value)&&BigInt(value)<=9223372036854775807n;
export function readMember(value:unknown,expected?:Pick<Member,'id'|'user_id'>):Member{
 if(!value||typeof value!=='object'||Array.isArray(value))throw new Error('MEMBER_RESPONSE_INVALID');
 const row=value as Record<string,unknown>;
 if(Object.keys(row).sort().join(',')!=='email,id,revision,role,status,user_id'||!uuid(row.id)||!uuid(row.user_id)||typeof row.email!=='string'||row.email.length<1||row.email.length>320||typeof row.role!=='string'||!['owner','admin','editor','viewer'].includes(row.role)||typeof row.status!=='string'||!['active','suspended'].includes(row.status)||!revision(row.revision)||expected&&(row.id!==expected.id||row.user_id!==expected.user_id))throw new Error('MEMBER_RESPONSE_INVALID');
 return structuredClone(row) as Member;
}
export function readMemberPage(value:unknown):Page<Member>{
 if(!value||typeof value!=='object'||Array.isArray(value))throw new Error('MEMBER_PAGE_INVALID');
 const page=value as Record<string,unknown>;
 if(Object.keys(page).some(key=>!['items','next_cursor','revision'].includes(key))||!Array.isArray(page.items)||page.items.length>100||!(page.next_cursor===null||typeof page.next_cursor==='string'&&page.next_cursor.length>0&&page.next_cursor.length<=4096)||page.revision!==undefined&&!revision(page.revision))throw new Error('MEMBER_PAGE_INVALID');
 const items=page.items.map(row=>readMember(row));
 if(new Set(items.map(row=>row.id)).size!==items.length||new Set(items.map(row=>row.user_id)).size!==items.length||page.next_cursor!==null&&items.length===0)throw new Error('MEMBER_PAGE_INVALID');
 return {items,next_cursor:page.next_cursor as string|null,...(page.revision!==undefined?{revision:page.revision as string}:{})};
}
