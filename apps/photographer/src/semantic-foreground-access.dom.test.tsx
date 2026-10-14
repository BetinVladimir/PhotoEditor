// @vitest-environment jsdom
import React from 'react';
import {act,cleanup,render,screen,waitFor} from '@testing-library/react';
import {afterEach,expect,it,vi} from 'vitest';
import {APIError,type SemanticTargetResource} from '@photoeditor/api-client';
const app=vi.hoisted(()=>({currentState:'active',listeners:new Set<(s:string)=>void>()}));
vi.mock('react-native',async()=>({...await import('react-native-web'),AppState:{get currentState(){return app.currentState},addEventListener:(_event:string,callback:(s:string)=>void)=>{app.listeners.add(callback);return{remove:()=>app.listeners.delete(callback)}}}}));
import {SemanticTargetScreen} from './semantic-target-screen';
const id='11111111-1111-4111-8111-111111111111';
const item:SemanticTargetResource={id,revision:'3',document:null,data:{resource_type:'semantic_targets',id,tenant_id:id,project_id:id,created_at:'2026-10-04T12:00:00Z',revision:'3',updated_at:'2026-10-04T12:00:00Z',intent_id:id,child_run_id:id,batch_id:id,asset_version_id:id,candidate_artifact_id:null,review_id:null,override_id:null,state:'applied'}};
afterEach(()=>{cleanup();app.currentState='active';app.listeners.clear()});
function foreground(state:string){act(()=>{app.currentState=state;for(const listener of app.listeners)listener(state)})}
it('revalidates a reviewable batch on foreground, removes revoked results and close authority, and reloads after access returns',async()=>{
 let denied=false;
 const status={run_id:id,workflow_id:id,revision:'4',state:'waiting_review' as const,plan_sha:'a'.repeat(64),remaining:0,can_close:true};
 const client={semanticTargets:vi.fn(async()=>({items:[item],next_cursor:null})),semanticReviewStatus:vi.fn(async()=>{if(denied)throw new APIError(403,'ACCESS_DENIED');return status}),closeSemanticReview:vi.fn()};
 render(<SemanticTargetScreen client={client} tenant={id} intent={id}/>);
 await waitFor(()=>expect(screen.getByText('Applied')).toBeTruthy());
 await waitFor(()=>expect(screen.getByText('Close review')).toBeTruthy());
 const before=client.semanticTargets.mock.calls.length;
 foreground('background');denied=true;foreground('active');
 await waitFor(()=>expect(screen.queryByText('Applied')).toBeNull());
 await waitFor(()=>expect(screen.queryByText('Close review')).toBeNull());
 expect(client.closeSemanticReview).not.toHaveBeenCalled();
 foreground('background');denied=false;foreground('active');
 await waitFor(()=>expect(screen.getByText('Applied')).toBeTruthy());
 await waitFor(()=>expect(screen.getByText('Close review')).toBeTruthy());
 expect(client.semanticTargets.mock.calls.length).toBeGreaterThan(before);
});
