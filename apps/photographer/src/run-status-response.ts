import type {RunStatus} from './catalog-types';
/** A receipt may be acknowledged only after its run identity and DTO are valid. */
export function readRunStatusResponse(raw:unknown,id:string):RunStatus{
 if(!raw||typeof raw!=='object'||Array.isArray(raw))throw new Error('RUN_STATUS_RESPONSE_INVALID');
 const value=raw as RunStatus;
 if(value.id!==id||typeof value.revision!=='string'||!/^[1-9][0-9]{0,18}$/.test(value.revision)||BigInt(value.revision)>9223372036854775807n
  ||typeof value.kind!=='string'||!value.kind||!['draft','queued','running','waiting_review','cancelling','cancelled','completed','completed_with_errors','failed'].includes(value.status)||typeof value.stage!=='string'
  ||typeof value.progress!=='number'||!Number.isFinite(value.progress)||value.progress<0||value.progress>1
  ||!value.counts||typeof value.counts!=='object'||Array.isArray(value.counts)
  ||!['total','succeeded','failed','skipped','review'].every(key=>{const count=value.counts[key as keyof RunStatus['counts']];return Number.isSafeInteger(count)&&count>=0;})
  ||!Array.isArray(value.errors)||value.errors.some(error=>typeof error!=='string')
  ||value.output_collection_id!==undefined&&(typeof value.output_collection_id!=='string'||!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(value.output_collection_id)||value.output_collection_id==='00000000-0000-0000-0000-000000000000')
  ||value.presentation!==undefined&&(!value.presentation||typeof value.presentation!=='object'||Array.isArray(value.presentation)||!['total','pending','ready','failed','cancelled','unavailable'].every(key=>{const count=value.presentation![key as keyof NonNullable<RunStatus['presentation']>];return Number.isSafeInteger(count)&&count>=0;})))throw new Error('RUN_STATUS_RESPONSE_INVALID');
 return structuredClone(value);
}
