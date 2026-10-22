// @vitest-environment jsdom
import {cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
import {afterEach,expect,it,vi} from 'vitest';
const handlers=vi.hoisted(()=>({apply:[] as Array<()=>void>}));
vi.mock('react-native-paper',async()=>({...await vi.importActual<typeof import('react-native-paper')>('react-native-paper'),
 View:({children}:any)=><div>{children}</div>,Text:({children}:any)=><span>{children}</span>,ActivityIndicator:()=>null,
 TextInput:({accessibilityLabel,value,onChangeText}:any)=><input aria-label={accessibilityLabel} value={value} onChange={e=>onChangeText(e.target.value)}/>,
 Button:({children,onPress}:any)=>{if(children==='Apply gallery filters')handlers.apply.push(onPress);return <button onClick={onPress}>{children}</button>;},
}));
vi.mock('./locale-context',async()=>{const __factory=()=>({useLocale:()=>({locale:'en'})});const __out=await __factory();const {withUILocaleDouble}=await import('./test/locale-double');return {...__out,useLocale:()=>withUILocaleDouble(__out.useLocale())};});
vi.mock('./use-review-foreground',()=>({useReviewForeground:()=>({active:true,generation:0})}));
import {CollectionGallery} from './collection-gallery';
import type {Collection} from './catalog-types';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const collection={id:id(3),tenant_id:id(2),project_id:id(9),name:'Gallery',description:'',state:'draft',sort_mode:'manual',created_at:'2026-10-17T00:00:00Z',revision:'1'} as Collection;
afterEach(()=>{cleanup();handlers.apply=[];});
it('an earlier Apply handler uses the latest filter draft',async()=>{
 const request=vi.fn().mockImplementation(async(path:string)=>path.includes('/items?')?{items:[],revision:'1'}:collection);
 render(<CollectionGallery client={{request} as any} tenant={id(2)} collection={collection} items={[]} current={()=>true}>{()=>null}</CollectionGallery>);
 await screen.findByText('0 / 0');const oldApply=handlers.apply[0];
 fireEvent.change(screen.getByLabelText('Search filename, tags or asset ID'),{target:{value:'latest'}});oldApply();
 await waitFor(()=>expect(request).toHaveBeenCalled());expect(request.mock.calls[0][0]).toContain('search=latest');
});
it('resets filters for a replaced client and refuses its old Apply callback',async()=>{
 const first=vi.fn().mockImplementation(async(path:string)=>path.includes('/items?')?{items:[],revision:'1'}:collection),second=vi.fn();
 const one={request:first} as any,two={request:second} as any;
 const view=render(<CollectionGallery client={one} tenant={id(2)} collection={collection} items={[]} current={()=>true}>{()=>null}</CollectionGallery>);
 await screen.findByText('0 / 0');fireEvent.change(screen.getByLabelText('Search filename, tags or asset ID'),{target:{value:'previous'}});const oldApply=handlers.apply.at(-1)!;
 view.rerender(<CollectionGallery client={two} tenant={id(2)} collection={collection} items={[]} current={()=>true}>{()=>null}</CollectionGallery>);
 await screen.findByText('0 / 0');expect((screen.getByLabelText('Search filename, tags or asset ID') as HTMLInputElement).value).toBe('');oldApply();expect(second).not.toHaveBeenCalled();expect(first).not.toHaveBeenCalled();
});
