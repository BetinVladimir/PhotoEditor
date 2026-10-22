// @vitest-environment jsdom
import {afterEach,expect,it,vi} from 'vitest';
import {cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('./use-review-foreground',()=>({useReviewForeground:()=>({active:true,generation:0})}));
vi.mock('./locale-context',async()=>{const __factory=()=>({useLocale:()=>({locale:'en'})});const __out=await __factory();const {withUILocaleDouble}=await import('./test/locale-double');return {...__out,useLocale:()=>withUILocaleDouble(__out.useLocale())};});
import {CollectionGallery} from './collection-gallery';
import type {Collection,CollectionItem} from './catalog-types';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const collection={id:id(3),tenant_id:id(2),project_id:id(9),name:'Gallery',description:'',state:'draft',sort_mode:'manual',created_at:'2026-10-17T00:00:00Z',revision:'1'} as Collection;
const row=(n:number)=>({id:id(n),collection_id:id(3),asset_id:id(n+10),asset_version_id:id(n+20),position:n,visibility:'visible',moderation_state:'accepted',rating:0,flag:'none',tags:[],created_at:'2026-10-17T00:00:00Z'} as CollectionItem);
afterEach(cleanup);
it('keeps full bulk source unchanged and fences an old filtered response after replacement',async()=>{
 let finish!:(v:unknown)=>void;const old=new Promise(resolve=>{finish=resolve});
 const items=[row(4),row(5)];const request=vi.fn().mockImplementation(async(path:string)=>path.endsWith('/items?limit=100&search=old')?old:path.includes('/items?')?{items:[row(5)],revision:'1',next_cursor:null}:collection);
 render(<CollectionGallery client={{request} as never} tenant={id(2)} collection={collection} items={items} current={()=>true}>{visible=><div data-testid="gallery">{visible.map(v=>v.id).join(',')}</div>}</CollectionGallery>);
 await screen.findByTestId('gallery');expect(screen.getByTestId('gallery').textContent).toContain(id(4));
 fireEvent.change(screen.getByLabelText('Search filename, tags or asset ID'),{target:{value:'old'}});fireEvent.click(screen.getByText('Apply gallery filters'));
 await waitFor(()=>expect(request).toHaveBeenCalledTimes(1));
 fireEvent.change(screen.getByLabelText('Search filename, tags or asset ID'),{target:{value:'new'}});fireEvent.click(screen.getByText('Apply gallery filters'));
 await waitFor(()=>expect(screen.getByTestId('gallery').textContent).toBe(id(5)));
 finish({items:[row(4)],revision:'1',next_cursor:null});await Promise.resolve();
 expect(screen.getByTestId('gallery').textContent).toBe(id(5));expect(items.map(v=>v.id)).toEqual([id(4),id(5)]);
});
it('exposes Stop and readonly Retry while metadata waits',async()=>{
 const request=vi.fn().mockImplementation(()=>new Promise(()=>{}));
 render(<CollectionGallery client={{request} as never} tenant={id(2)} collection={collection} items={[row(4)]} current={()=>true}>{visible=><div>{visible.length}</div>}</CollectionGallery>);
 await screen.findByText('1 / 1');fireEvent.click(screen.getByText('Pending review'));fireEvent.click(screen.getByText('Apply gallery filters'));
 fireEvent.click(await screen.findByText('Stop gallery loading'));await screen.findByText('Retry gallery');fireEvent.click(screen.getByText('Retry gallery'));
 await waitFor(()=>expect(request).toHaveBeenCalledTimes(2));expect(request.mock.calls.every(([,options])=>!options.method)).toBe(true);
});
