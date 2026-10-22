import {useLocale} from './locale-context';
import {Button,Text} from 'react-native-paper';
import {useReadResource} from './use-read-resource';
import {ReadResourceState} from './read-resource-state';
import {useMemo,useState} from 'react';
import {View} from 'react-native';
import {APIError} from '@photoeditor/api-client';
import type {OwnerAPI} from './owner-api';
import {readLocalPortraitDiscovery,type LocalPortraitAutoAppliedVersion} from './local-portrait-result';
import {LocalPortraitRunResult} from './local-portrait-run-result';
type Props={api:OwnerAPI;run:string;onOpen:(result:LocalPortraitAutoAppliedVersion)=>void;onAccessDenied:()=>void};
export function LocalPortraitRunDiscovery(props:Props){const scope=useMemo(()=>crypto.randomUUID(),[props.api,props.run]);return <Discovery key={scope} {...props}/>;}
function Discovery(props:Props){
 const __uiT=useLocale().t;

 const[denied,setDenied]=useState(false);
 const resource=useReadResource(JSON.stringify(['local-portrait',props.run]),props.api,async signal=>{
  try{const value=readLocalPortraitDiscovery(await props.api.discoverLocalPortraitResult(props.run,signal),props.run);signal.throwIfAborted();return value;}catch(error){if(!signal.aborted&&error instanceof APIError&&[401,403,404,410].includes(error.status)){setDenied(true);props.onAccessDenied();}throw error;}
 });
 if(denied)return null;
 if(resource.phase==='error')return <View><Text accessibilityRole="alert">{__uiT('ui_7eb3ddae40ed')}</Text><Button accessibilityRole="button" onPress={resource.retry} mode="outlined">{__uiT('ui_e5f37c95b62e')}</Button></View>;
 if(resource.phase!=='ready')return <ReadResourceState phase={resource.phase} retry={resource.retry} cancel={resource.cancel}/>;
 const workflow=resource.data?.workflow_id;
 return workflow?<LocalPortraitRunResult api={props.api} run={props.run} workflow={workflow} onOpen={props.onOpen} onAccessDenied={props.onAccessDenied}/>:null;
}
