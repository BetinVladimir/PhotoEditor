import {expect} from 'vitest';
import {act,fireEvent,waitFor} from '@testing-library/react';
const controls='button,input,textarea,select,[role="button"],[role="checkbox"],[role="radio"],[role="switch"]';
/** Both browser-native and accessibility disabled states must permit an action. */
export function expectEnabled(node:Element|null|undefined){
 if(!node)throw new Error('The expected interactive control has not mounted');
 expect(node.isConnected).toBe(true);
 const control=node.closest(controls);
 if(!control)throw new Error('The expected element is not an interactive control');
 expect(control.matches(':disabled')).toBe(false);
 expect(control.hasAttribute('disabled')).toBe(false);
 expect(control.getAttribute('aria-disabled')).not.toBe('true');
}
/** Positive user action; deliberately disabled/stale-action tests use raw events. */
export async function clickWhenEnabled(node:Element){
 await waitFor(()=>expectEnabled(node));
 await act(async()=>{});
 expectEnabled(node);
 fireEvent.click(node);
}
