// @vitest-environment jsdom
import {useLayoutEffect} from 'react';
import {cleanup,render} from '@testing-library/react';
import {afterEach,expect,it,vi} from 'vitest';
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('./locale-context',async()=>{const __factory=()=>({useLocale:()=>({t:(key:string)=>key})});const __out=await __factory();const {withUILocaleDouble}=await import('./test/locale-double');return {...__out,useLocale:()=>withUILocaleDouble(__out.useLocale())};});
import {ReadResourceState} from './read-resource-state';
afterEach(cleanup);
it('dispatches retry rather than the prior cancel handler as soon as the error action is committed',()=>{
 const retry=vi.fn(),cancel=vi.fn();
 function CommittedClick({error}:{error:boolean}){
  useLayoutEffect(()=>{if(error){const button=document.querySelector('button');expect(button?.textContent).toContain('read_retry');button!.click();}},[error]);
  return null;
 }
 function Harness({error}:{error:boolean}){return <><ReadResourceState phase={error?'error':'loading'} retry={retry} cancel={cancel}/><CommittedClick error={error}/></>}
 const view=render(<Harness error={false}/>);view.rerender(<Harness error/>);
 expect({retry:retry.mock.calls.length,cancel:cancel.mock.calls.length}).toEqual({retry:1,cancel:0});
});
