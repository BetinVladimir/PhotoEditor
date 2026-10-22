import type {Page} from './catalog-types';
import {readSessionAnalysis} from './session-analysis-response';
export type SessionAnalysisSummary={id:string;sessionId:string;createdAt:string;plan:{artifact_id:string;sha256:string}};
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
export function readSessionAnalyses(value:unknown,sessionId:string,cursor?:string,tenant?:string):Page<SessionAnalysisSummary>{
 if(!uuid(sessionId)||!value||typeof value!=='object')throw new Error('SESSION_ANALYSES_INVALID');
 const page=value as Page<unknown>;if(!Object.hasOwn(value,'next_cursor')||!Array.isArray(page.items)||page.items.length>100||(page.next_cursor!==null&&(typeof page.next_cursor!=='string'||page.next_cursor.length>2048||page.next_cursor.length<1||page.next_cursor===cursor)))throw new Error('SESSION_ANALYSES_INVALID');
 const ids=new Set<string>();const items=page.items.map(value=>{
  if(!value||typeof value!=='object')throw new Error('SESSION_ANALYSES_INVALID');
  let envelope:ReturnType<typeof readSessionAnalysis>;try{envelope=readSessionAnalysis(value,{session:sessionId,tenant});}catch{throw Error('SESSION_ANALYSES_INVALID');}
  const data=envelope.data,document=envelope.document;
  if(!uuid(envelope.id)||ids.has(envelope.id)||envelope.revision!=='1'||!data||data.id!==envelope.id||data.resource_type!=='session_analyses'||data.session_id!==sessionId||typeof data.created_at!=='string'||!Number.isFinite(Date.parse(data.created_at))||!document||!uuid(document.artifact_id)||data.plan_artifact_id!==document.artifact_id||typeof document.sha256!=='string'||!/^[0-9a-f]{64}$/.test(document.sha256))throw new Error('SESSION_ANALYSES_INVALID');
  ids.add(envelope.id);return {id:envelope.id,sessionId,createdAt:data.created_at,plan:{artifact_id:document.artifact_id,sha256:document.sha256}};
 });
 return {items,next_cursor:page.next_cursor??null};
}
