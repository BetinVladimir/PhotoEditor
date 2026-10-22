// @vitest-environment jsdom
import {afterEach,expect,it,vi} from 'vitest';
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('./locale-context',async()=>{const __factory=()=>({useLocale:()=>({t:(key:string)=>key})});const __out=await __factory();const {withUILocaleDouble}=await import('./test/locale-double');return {...__out,useLocale:()=>withUILocaleDouble(__out.useLocale())};});
import {SemanticShownVersionAction} from './semantic-shown-version-action';
const id='11111111-1111-4111-8111-111111111111';
afterEach(cleanup);
it('cancels materialization waiting and refuses its late version while a new target-derived retry owns UI',async()=>{
 let first!:(value:{asset_version_id:string;asset_id:string})=>void,second!:(value:{asset_version_id:string;asset_id:string})=>void;
 const materializeSemanticShownVersion=vi.fn().mockImplementationOnce(()=>new Promise(resolve=>{first=resolve})).mockImplementationOnce(()=>new Promise(resolve=>{second=resolve})),onOpen=vi.fn();
 render(<SemanticShownVersionAction client={{materializeSemanticShownVersion}} tenant={id} target={id} onOpen={onOpen}/>);
 fireEvent.click(screen.getByText('semantic_version_open'));await waitFor(()=>expect(materializeSemanticShownVersion).toHaveBeenCalledOnce());fireEvent.click(screen.getByText('read_cancel'));fireEvent.click(screen.getByText('semantic_version_open'));
 await waitFor(()=>expect(materializeSemanticShownVersion).toHaveBeenCalledTimes(2));await act(async()=>first({asset_version_id:id,asset_id:id}));expect(onOpen).not.toHaveBeenCalled();expect(screen.getByText('read_cancel')).toBeTruthy();
 await act(async()=>second({asset_version_id:id,asset_id:id}));expect(onOpen).toHaveBeenCalledOnce();
});
