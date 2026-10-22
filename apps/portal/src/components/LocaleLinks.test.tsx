import {afterEach,expect,it} from 'vitest';
import React,{act} from 'react';
import {renderToStaticMarkup} from 'react-dom/server';
import {createRoot,type Root} from 'react-dom/client';
import {JSDOM} from 'jsdom';
import {LocaleLinks} from './LocaleLinks';
import {Portal} from './Portal';
const share='48cf689a-c8c0-4f70-83a7-982fd95fd946';
let root:Root|undefined,dom:JSDOM|undefined;
afterEach(async()=>{if(root)await act(async()=>root!.unmount());root=undefined;dom?.window.close();dom=undefined;});
it.each(['ru','en','bg'] as const)('loading shell keeps language choice and Framevela brand for %s',locale=>{
 const html=renderToStaticMarkup(<Portal locale={locale} shareID={share}/>);
 expect(html).toContain('<h1>Framevela</h1>');
 for(const language of ['ru','en','bg'])expect(html).toContain(`href="/${language}/s/${share}"`);
});
it('updates every locale link after consumed fragment removal without losing repeated query values',async()=>{
 dom=new JSDOM('<div id="host"></div>',{url:`https://framevela.com/en/s/${share}?x=a%2Bb&x=2#test-secret`});
 for(const key of ['window','document','HTMLElement','Node','MouseEvent'])Object.defineProperty(globalThis,key,{configurable:true,value:(dom.window as any)[key]});
 (globalThis as any).IS_REACT_ACT_ENVIRONMENT=true;
 root=createRoot(document.getElementById('host')!);
 await act(async()=>root!.render(<LocaleLinks locale="en" path={`/en/s/${share}`} label="Language"/>));
 const links=()=>Array.from(document.querySelectorAll('a')).map(a=>a.getAttribute('href'));
 expect(links()).toEqual(['ru','bg','en'].map(l=>`/${l}/s/${share}?x=a%2Bb&x=2#test-secret`));
 await act(async()=>{dom!.window.history.replaceState(null,'',`/en/s/${share}?x=a%2Bb&x=2`);dom!.window.dispatchEvent(new dom!.window.Event('portal-location-change'));});
 expect(links()).toEqual(['ru','bg','en'].map(l=>`/${l}/s/${share}?x=a%2Bb&x=2`));
});
