import {useEffect,useState} from 'react';
import {AppState} from 'react-native';

/** Unmount authorized media/actions in background; remount with fresh authority. */
export function useReviewForeground(){
 const [state,setState]=useState(()=>({active:(AppState.currentState??'active')==='active',generation:0}));
 useEffect(()=>{
  const subscription=AppState.addEventListener('change',value=>setState(previous=>({active:value==='active',generation:previous.generation+(value==='active'&&!previous.active?1:0)})));
  return()=>subscription.remove();
 },[]);
 return state;
}
