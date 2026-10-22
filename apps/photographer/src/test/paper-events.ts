/** Dispatch interactions after React's pending passive effects.
 * RNWeb PressResponder receives its latest disabled/onPress configuration in a
 * passive effect. A DOM-only readiness check can precede that update.
 * Finish the previous React effect scope before entering the next event scope;
 * an empty flushSync outside act can leave the old responder configuration live.
 * This neither retries events nor replaces Paper or its authority guards.
 */
if(typeof document!=='undefined'){
 const [{configure,act},{flushSync}]=await Promise.all([
  import('@testing-library/react'),import('react-dom'),
 ]);
 let dispatching=false;
 configure({eventWrapper:callback=>{
  if(dispatching)return callback();
  let result:ReturnType<typeof callback>;
  dispatching=true;
  try{act(()=>{flushSync(()=>{});});act(()=>{result=callback();});return result!;}
  finally{dispatching=false;}
 }});
}
export {};
