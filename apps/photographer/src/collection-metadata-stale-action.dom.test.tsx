// @vitest-environment jsdom
import {cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
import {afterEach,expect,it,vi} from 'vitest';
const retained=vi.hoisted(()=>({save:[] as Array<()=>void>}));
vi.mock('react-native-paper',async()=>({...await vi.importActual<typeof import('react-native-paper')>('react-native-paper'),
 View:({children}:any)=><div>{children}</div>,
 Text:({children}:any)=><span>{children}</span>,
 TextInput:({accessibilityLabel,value,onChangeText,editable}:any)=><input aria-label={accessibilityLabel} value={value} disabled={!editable} onChange={e=>onChangeText(e.target.value)}/>,
 Button:({children,onPress,disabled}:any)=>{if(children==='Save collection settings')retained.save.push(onPress);return <button disabled={disabled} onClick={onPress}>{children}</button>;},
}));
vi.mock('./locale-context',async()=>{const __factory=()=>({useLocale:()=>({locale:'en'})});const __out=await __factory();const {withUILocaleDouble}=await import('./test/locale-double');return {...__out,useLocale:()=>withUILocaleDouble(__out.useLocale())};});
vi.mock('./use-review-foreground',()=>({useReviewForeground:()=>({active:true,generation:0})}));
import {CollectionMetadata,type CollectionMetadataProps} from './collection-metadata';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const row={id:id(3),tenant_id:id(1),project_id:id(2),name:'Wedding',description:'',state:'draft',revision:'1',sort_mode:'manual',created_at:'2026-10-17T10:00:00Z'} as const;
const props=(request:any):CollectionMetadataProps=>({client:{request} as any,tenant:id(1),collection:row,items:[],canEdit:true,current:()=>true,acquire:()=>Symbol(),release:vi.fn(),onChanged:vi.fn()});
afterEach(()=>{cleanup();retained.save=[];});
it('an earlier save handler submits the current edited draft',async()=>{
 const request=vi.fn().mockResolvedValue({...row,name:'Latest edit',revision:'2'});render(<CollectionMetadata {...props(request)}/>);const oldSave=retained.save[0];
 fireEvent.change(screen.getByLabelText('Collection name'),{target:{value:'Latest edit'}});oldSave();
 await waitFor(()=>expect(request).toHaveBeenCalledTimes(1));expect(request.mock.calls[0][1].body.name).toBe('Latest edit');
});
it('an earlier save handler cannot repeat a mutation after an uncertain result',async()=>{
 const request=vi.fn().mockRejectedValue(new Error('connection lost'));render(<CollectionMetadata {...props(request)}/>);const oldSave=retained.save[0];fireEvent.click(screen.getByText('Save collection settings'));
 await screen.findByText('Unable to confirm. Reload reads current state without repeating the change.');oldSave();expect(request).toHaveBeenCalledTimes(1);
});
