import {APIError} from '@photoeditor/api-client';
import type {OwnerAPI} from './owner-api';
import type {CaptureShotListView} from './capture-types';

// Recovery supplies a CAS revision, never the forbidden document or identities.
// The caller must keep the editor blocked until explicit replacement intent.
export async function loadCaptureShots(api:Pick<OwnerAPI,'captureShotList'|'get'>,sessionId:string,signal?:AbortSignal):Promise<{list:CaptureShotListView;blocked:boolean}>{
 try{return {list:await api.captureShotList(sessionId,signal),blocked:false}}
 catch(error){
  if(!(error instanceof APIError)||error.status!==409||error.code!=='CONSENT_REVOKED')throw error;
  const session=await api.get<{revision:string}>(`/api/v1/tenants/{t}/shoot-sessions/${encodeURIComponent(sessionId)}`,signal);
  if(!/^[1-9][0-9]*$/.test(session.revision)||BigInt(session.revision)>9007199254740991n)throw new Error('Invalid session revision');
  return {list:{session_id:sessionId,revision:session.revision,document:null,requirements:[]},blocked:true};
 }
}
