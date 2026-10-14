import type {ShootSession,ShootSessionUpdate} from './capture-types';
import {SessionPolicyDraftStore,parseSessionPolicyAttempt,type SessionPolicyLocal,type SessionPolicyDraft} from './session-policy-draft';
/** Read before accepting edits; existing drafts require an explicit restore/discard decision. */
export class SessionPolicyPersistence{
 private attempt:SessionPolicyDraft['attempt'];private local:SessionPolicyLocal|null=null;private uncertain=false;private loaded:SessionPolicyDraft|null=null;private ready=false;private decided=false;private closed=false;private edited=false;
 constructor(private store:SessionPolicyDraftStore,private session:ShootSession){this.session=structuredClone(session);}
 async load():Promise<SessionPolicyDraft|null>{const draft=await this.store.read();if(this.closed)return null;this.loaded=draft?structuredClone(draft):null;this.ready=true;this.decided=!draft;return draft;}
 async discard(){if(!this.ready||this.closed)throw new Error('SESSION_DRAFT_NOT_READY');await this.store.clear();if(!this.closed)this.decided=true;}
 restore():SessionPolicyLocal{
  const draft=this.loaded;
  if(!this.ready||this.closed||this.decided||!draft||draft.uncertain||draft.session_id!==this.session.id||draft.base_revision!==this.session.revision||!draft.local)throw new Error('SESSION_DRAFT_REQUIRES_COMPARISON');
  this.decided=true;this.edited=true;this.local=structuredClone(draft.local);return structuredClone(draft.local);
 }
 restoreCompared():SessionPolicyLocal{
  const draft=this.loaded;if(!this.ready||this.closed||this.decided||!draft?.local||draft.uncertain||this.session.state!=='open')throw new Error('SESSION_DRAFT_REQUIRES_COMPARISON');
  const old=draft.policy,current=this.session.policy,local=draft.local;
  const offsetsChanged=JSON.stringify(local.offsets)!==JSON.stringify(old.camera_offsets.map(v=>({...v,seconds:String(v.seconds)})));
  const merged={...structuredClone(local),name:local.name===draft.name?this.session.name:local.name,gap:local.gap===String(old.time_gap_seconds)?String(current.time_gap_seconds):local.gap,neighbors:local.neighbors===String(old.neighbor_count)?String(current.neighbor_count):local.neighbors,protect:local.protect===old.protect_manual_picks?current.protect_manual_picks:local.protect,offsets:offsetsChanged?structuredClone(local.offsets):current.camera_offsets.map(v=>({...v,seconds:String(v.seconds)}))};
  this.decided=true;this.edited=true;this.local=structuredClone(merged);return merged;
 }
 async changed(local:SessionPolicyLocal|null){
  if(this.closed)return;if(!this.ready||!this.decided)throw new Error('SESSION_DRAFT_NOT_READY');
  if(!local){if(this.uncertain)return;if(this.edited){await this.store.clear();this.edited=false;}return;}
  this.local=structuredClone(local);this.edited=true;await this.store.write({schema_version:1,session_id:this.session.id,base_revision:this.session.revision,name:this.session.name,policy:structuredClone(this.session.policy),local:structuredClone(local),uncertain:this.uncertain,...(this.attempt?{attempt:structuredClone(this.attempt)}:{}),saved_at:Date.now()});
 }
 async markUncertain(){if(!this.ready||!this.decided||!this.local||this.closed)throw new Error('SESSION_DRAFT_NOT_READY');this.uncertain=true;await this.changed(this.local);}
 async prepare(request:ShootSessionUpdate,key:string){const parsed=parseSessionPolicyAttempt({request,key},this.session.id);if(!parsed)throw new Error('SESSION_ATTEMPT_INVALID');if(this.attempt&&(this.attempt.key!==key||JSON.stringify(this.attempt.request)!==JSON.stringify(parsed.request)))throw new Error('SESSION_ATTEMPT_CHANGED');this.attempt=parsed;await this.markUncertain();const persisted=await this.store.read();if(this.closed||persisted?.attempt?.key!==key||JSON.stringify(persisted.attempt.request)!==JSON.stringify(parsed.request))throw new Error('SESSION_ATTEMPT_NOT_PERSISTED');}
 async releaseRejectedAttempt(){if(this.closed)return;this.attempt=undefined;this.uncertain=false;if(this.local)await this.changed(this.local);}
 async rejectLoadedAttempt(){if(this.closed||!this.loaded?.attempt)throw new Error('SESSION_DRAFT_NOT_READY');const draft={...structuredClone(this.loaded),uncertain:false,saved_at:Date.now()};delete draft.attempt;await this.store.write(draft);this.loaded=structuredClone(draft);return draft;}
 async complete(key:string){return this.store.clearAttempt(key);}
 close(){this.closed=true;}
}
