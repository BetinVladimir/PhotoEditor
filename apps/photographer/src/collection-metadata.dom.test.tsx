// @vitest-environment jsdom
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
import {afterEach,expect,it,vi} from 'vitest';
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('./locale-context',async()=>{const __factory=()=>({useLocale:()=>({locale:'en'})});const __out=await __factory();const {withUILocaleDouble}=await import('./test/locale-double');return {...__out,useLocale:()=>withUILocaleDouble(__out.useLocale())};});
const fg=vi.hoisted(()=>({active:true,generation:0}));vi.mock('./use-review-foreground',()=>({useReviewForeground:()=>fg}));
import {CollectionMetadata,type CollectionMetadataProps} from './collection-metadata';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const row=()=>({id:id(3),tenant_id:id(1),project_id:id(2),name:'Wedding',description:'',state:'draft',revision:'1',sort_mode:'manual',created_at:'2026-10-17T10:00:00Z'} as const);
const props=(request:any):CollectionMetadataProps=>({client:{request} as any,tenant:id(1),collection:row(),items:[],canEdit:true,current:()=>true,acquire:()=>Symbol(),release:vi.fn(),onChanged:vi.fn()});
afterEach(()=>{cleanup();fg.active=true;fg.generation=0;});
it('stops an uncertain PATCH and reconciles through GET without repeating the mutation',async()=>{
 let resolve!:(v:unknown)=>void;const request=vi.fn().mockImplementation((_path,options)=>options?.method==='PATCH'?new Promise(done=>resolve=done):Promise.resolve({...row(),name:'Changed',revision:'2'})),p=props(request);render(<CollectionMetadata {...p}/>);fireEvent.change(screen.getByLabelText('Collection name'),{target:{value:'Changed'}});fireEvent.click(screen.getByText('Save collection settings'));await waitFor(()=>expect(request).toHaveBeenCalledTimes(1));fireEvent.click(screen.getByText('Stop waiting'));await act(async()=>resolve({...row(),name:'Changed',revision:'2'}));expect(p.onChanged).not.toHaveBeenCalled();fireEvent.click(screen.getByText('Reload collection settings'));await waitFor(()=>expect(p.onChanged).toHaveBeenCalledTimes(1));expect(request.mock.calls.filter(([,o])=>o?.method==='PATCH')).toHaveLength(1);expect(request.mock.calls.at(-1)![1].method).toBeUndefined();
});
it('requires explicit archive confirmation and treats the reply as queued cleanup',async()=>{
 const request=vi.fn().mockResolvedValue({job_id:id(9),status:'queued'}),p=props(request);render(<CollectionMetadata {...p}/>);fireEvent.click(screen.getByText('Archive and request deletion'));expect(request).not.toHaveBeenCalled();fireEvent.click(screen.getByText('Confirm archive and deletion request'));await waitFor(()=>expect(p.onChanged).toHaveBeenCalledTimes(1));expect(request.mock.calls[0][1].method).toBe('DELETE');await screen.findByText('Archived; deletion request queued. Cleanup is asynchronous.');
});
it('withdraws in-flight role authority and ignores the late mutation response',async()=>{
 let resolve!:(v:unknown)=>void;const request=vi.fn().mockImplementation(()=>new Promise(done=>resolve=done)),p=props(request),view=render(<CollectionMetadata {...p}/>);fireEvent.click(screen.getByText('Save collection settings'));await waitFor(()=>expect(request).toHaveBeenCalledTimes(1));view.rerender(<CollectionMetadata {...p} canEdit={false}/>);expect(request.mock.calls[0][1].signal.aborted).toBe(true);await act(async()=>resolve({...row(),revision:'2'}));expect(p.onChanged).not.toHaveBeenCalled();
});
it('publishes the collection through its dedicated endpoint without changing selected photos',async()=>{
 const request=vi.fn().mockResolvedValue({...row(),state:'published',revision:'2'}),p=props(request);render(<CollectionMetadata {...p}/>);
 fireEvent.click(screen.getByText('Publish collection'));await waitFor(()=>expect(p.onChanged).toHaveBeenCalledTimes(1));
 expect(request.mock.calls[0][0]).toContain('/publication');expect(request.mock.calls[0][1].body).toEqual({expected_revision:1,item_ids:[],publish:true,publish_collection:true});
 fireEvent.click(screen.getByText('Unpublish collection'));await waitFor(()=>expect(request).toHaveBeenCalledTimes(2));
 expect(request.mock.calls[1][1].body).toEqual({expected_revision:2,item_ids:[],publish:false,publish_collection:true});
});
