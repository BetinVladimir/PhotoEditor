// @vitest-environment jsdom
import React from 'react';
import {act,render,screen,waitFor,fireEvent,cleanup} from '@testing-library/react';
import {afterEach,it,expect,vi} from 'vitest';
const app=vi.hoisted(()=>({state:'active',listeners:new Set<(state:string)=>void>()}));
vi.mock('react-native',async()=>({...await import('react-native-web'),AppState:{get currentState(){return app.state},addEventListener:(_event:string,listener:(state:string)=>void)=>{app.listeners.add(listener);return{remove:()=>app.listeners.delete(listener)}}}}));
import {SemanticReviewCloseControl} from './semantic-review-close';
const id='11111111-1111-4111-8111-111111111111';
const status={run_id:id,workflow_id:id,revision:'9',state:'waiting_review',plan_sha:'a'.repeat(64),remaining:0,can_close:true};
function change(state:string){act(()=>{app.state=state;app.listeners.forEach(listener=>listener(state))})}
afterEach(()=>{cleanup();app.state='active';app.listeners.clear()});
it('performs no initial read or close while mounted in background',async()=>{
 app.state='background';
 const client={semanticReviewStatus:vi.fn().mockResolvedValue(status),closeSemanticReview:vi.fn()};
 render(<SemanticReviewCloseControl client={client} tenant={id} intent={id}/>);
 await act(async()=>{});
 expect(client.semanticReviewStatus).not.toHaveBeenCalled();expect(client.closeSemanticReview).not.toHaveBeenCalled();
 expect(screen.queryByText('Close review')).toBeNull();
 change('active');
 await waitFor(()=>expect(screen.getByRole('button',{name:'Close review'}).hasAttribute('disabled')).toBe(false));
 expect(client.semanticReviewStatus).toHaveBeenCalledOnce();
});
it('acknowledges already completed review on return without resending uncertain close',async()=>{
 const client={semanticReviewStatus:vi.fn().mockResolvedValueOnce(status).mockResolvedValueOnce({...status,state:'completed',revision:'10',plan_sha:null,can_close:false}),closeSemanticReview:vi.fn().mockRejectedValueOnce(new Error('lost'))};
 render(<SemanticReviewCloseControl client={client} tenant={id} intent={id}/>);
 fireEvent.click(await screen.findByText('Close review'));
 await screen.findByText('Retry closing review');
 change('background');change('active');
 await screen.findByText(/Review closed/);
 expect(screen.queryByText('Retry closing review')).toBeNull();
 expect(client.closeSemanticReview).toHaveBeenCalledOnce();
 expect(client.semanticReviewStatus).toHaveBeenCalledTimes(2);
});
