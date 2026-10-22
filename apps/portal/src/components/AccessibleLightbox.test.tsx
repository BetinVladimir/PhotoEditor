import {describe,it,expect,beforeEach,afterEach}from 'vitest';
import {JSDOM}from 'jsdom';
import React,{act}from 'react';import {createRoot,type Root}from 'react-dom/client';
import {AccessibleLightbox}from './AccessibleLightbox';
let dom:JSDOM,root:Root,host:HTMLElement,opener:HTMLButtonElement,fallback:HTMLElement;
beforeEach(()=>{
 dom=new JSDOM('<body><main><h1 tabindex="-1">Gallery</h1><button id="open">Photo</button><div id="host"></div><aside aria-hidden="false">Existing</aside></main></body>',{url:'http://localhost'});
 for(const key of ['window','document','HTMLElement','Node','MutationObserver','getComputedStyle','KeyboardEvent','FocusEvent'])Object.defineProperty(globalThis,key,{configurable:true,value:(dom.window as any)[key]});
 (globalThis as any).IS_REACT_ACT_ENVIRONMENT=true;host=document.getElementById('host')!;opener=document.getElementById('open')as HTMLButtonElement;fallback=document.querySelector('h1')!;opener.focus();root=createRoot(host);
});
afterEach(async()=>{await act(async()=>root.unmount());dom.window.close();});
async function render(close=()=>{},name='Photo 1'){await act(async()=>root.render(<AccessibleLightbox name={name}onClose={close}restoreFallback={()=>fallback}><button>Close</button><button disabled>Unavailable</button><button>Next</button></AccessibleLightbox>));}
function key(value:string,shiftKey=false){document.activeElement!.dispatchEvent(new KeyboardEvent('keydown',{key:value,shiftKey,bubbles:true,cancelable:true}));}
describe('actual React DOM lightbox (jsdom, no browser acceptance)',()=>{
 it('enters, traps in both directions and skips disabled controls',async()=>{await render();const buttons=host.querySelectorAll('button');expect(document.activeElement).toBe(buttons[0]);key('Tab',true);expect(document.activeElement).toBe(buttons[2]);key('Tab');expect(document.activeElement).toBe(buttons[0]);key('Tab');expect(document.activeElement).toBe(buttons[2]);});
 it('Escape calls latest callback, preserves focus during rerenders',async()=>{let old=0,next=0;await render(()=>old++);(host.querySelectorAll('button')[2]as HTMLElement).focus();await render(()=>next++,'Updated image');expect(document.activeElement?.textContent).toBe('Next');key('Escape');expect([old,next]).toEqual([0,1]);});
 it('background inert/aria restoration exact, restores opener',async()=>{await render();const aside=document.querySelector('aside')!;expect(aside.hasAttribute('inert')).toBe(true);expect(aside.getAttribute('aria-hidden')).toBe('true');await act(async()=>root.render(null));expect(aside.hasAttribute('inert')).toBe(false);expect(aside.getAttribute('aria-hidden')).toBe('false');expect(document.activeElement).toBe(opener);});
 it('virtualized removed opener uses gallery heading fallback',async()=>{await render();opener.remove();await act(async()=>root.render(null));expect(document.activeElement).toBe(fallback);});
 it('new async background sibling inert and programmatic escape contained',async()=>{await render();const button=document.createElement('button');button.textContent='Async';document.querySelector('main')!.append(button);await act(async()=>{await Promise.resolve();});expect(button.hasAttribute('inert')).toBe(true);button.focus();expect(document.activeElement?.textContent).toBe('Close');});
 it('skips controls hidden by CSS ancestors and recovers empty controls',async()=>{await act(async()=>root.render(<AccessibleLightbox name="Image"onClose={()=>{}}restoreFallback={()=>fallback}><div style={{display:'none'}}><button>Hidden</button></div><button>Visible</button></AccessibleLightbox>));expect(document.activeElement?.textContent).toBe('Visible');key('Tab');expect(document.activeElement?.textContent).toBe('Visible');await act(async()=>root.render(<AccessibleLightbox name="Image"onClose={()=>{}}restoreFallback={()=>fallback}><p>No actions</p></AccessibleLightbox>));key('Tab');expect(document.activeElement?.getAttribute('role')).toBe('dialog');});
 it('retains preexisting inert attribute after cleanup',async()=>{const aside=document.querySelector('aside')!;aside.setAttribute('inert','preserved');await render();await act(async()=>root.render(null));expect(aside.getAttribute('inert')).toBe('preserved');});
});
