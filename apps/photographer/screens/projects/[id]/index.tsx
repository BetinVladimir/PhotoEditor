import {Button, Card, TouchableRipple, Text} from "react-native-paper";
import {CollectionCreateWizard} from '../../../src/collection-create-wizard';
import {useWorkspaceAccess} from '../../../src/workspace-access-context';
import {ReadResourceState} from "../../../src/read-resource-state";
import {useReviewForeground} from '../../../src/use-review-foreground';
import {projectCollectionPages} from '../../../src/project-collection-pages';
import {readProject} from '../../../src/catalog-read-response';
import {useLocale} from '../../../src/locale-context';
import{useEffect,useMemo,useState,useRef}from"react";import{router,useLocalSearchParams}from'../../../src/localized-router';import {FlatList, SafeAreaView, View} from "react-native";import{useAuth}from"../../../src/auth-context";import type{Collection,Page,Project}from"../../../src/catalog-types";import{catalogStyle as s}from"../../../src/catalog-style";
function ProjectScope(){
 const __r2T=useLocale().t;const __r2Value=useLocale().value;

 const __uiMessage=useLocale().message;

 const __uiT=useLocale().t;
const{t}=useLocale();const{id,tenant}=useLocalSearchParams<{id:string;tenant:string}>();const auth=useAuth();const workspace=useWorkspaceAccess(tenant||'');const client=useMemo(()=>auth.client(),[auth]);const life=useRef<AbortController|null>(null);const[project,setProject]=useState<Project>();const[items,setItems]=useState<Collection[]>([]);const[busy,setBusy]=useState(true);const[readRevision,setReadRevision]=useState(0);const[readPhase,setReadPhase]=useState<"loading"|"ready"|"error"|"cancelled">("loading");const[error,setError]=useState("");const root=`/api/v1/tenants/${encodeURIComponent(tenant||"")}`;const canEdit=workspace?.canEdit===true&&project?.can_edit===true;useEffect(()=>{
 const controller=new AbortController();life.current=controller;
 setProject(undefined);setItems([]);setError('');setBusy(true);setReadPhase('loading');
 if(!id||!tenant){setError('Project is required');setBusy(false);setReadPhase('error');return()=>controller.abort();}
 const readTimeout=setTimeout(()=>{if(life.current===controller&&!controller.signal.aborted){setReadPhase("error");setBusy(false);controller.abort();}},8000);
 void (async()=>{
  try{
   const rawProject=await client.request<unknown>(`${root}/projects/${encodeURIComponent(id)}`,{signal:controller.signal});
   controller.signal.throwIfAborted();
   const p=readProject(rawProject,tenant,id,true);
   const collections=await projectCollectionPages(id,cursor=>client.request<Page<Collection>>(`${root}/collections?limit=100&project_id=${encodeURIComponent(id)}${cursor?`&cursor=${encodeURIComponent(cursor)}`:''}`,{signal:controller.signal}),controller.signal,tenant);
   if(!controller.signal.aborted){setProject(p);setItems(collections);setReadPhase("ready");}
  }catch{if(!controller.signal.aborted){setError('Unable to load project');setReadPhase('error');}}
  finally{clearTimeout(readTimeout);if(!controller.signal.aborted)setBusy(false);}
 })();
 return()=>{clearTimeout(readTimeout);controller.abort();if(life.current===controller)life.current=null;};
},[id,tenant,client,root,auth.origin,readRevision,workspace?.actor,workspace?.canEdit]);if(readPhase!=="ready")return <SafeAreaView style={s.root}><ReadResourceState phase={readPhase} retry={()=>setReadRevision(value=>value+1)} cancel={()=>{life.current?.abort();setBusy(false);setReadPhase("cancelled");}}/></SafeAreaView>;return <SafeAreaView style={s.root}><FlatList style={{flex:1}} data={items} keyExtractor={x=>x.id} contentContainerStyle={s.list} ListHeaderComponent={<View style={s.header}><Text variant="headlineMedium" accessibilityRole="header" style={s.title}>{project?.name||__r2T('ui_985959785319')}</Text><Text style={s.subtitle}>{project?.description}</Text><Button mode="outlined" accessibilityRole="button" disabled={!canEdit} onPress={()=>router.push({pathname:"/imports/new",params:{tenant,project:id}})}>{__uiT('ui_6b071abc6cf0')}</Button><Button mode="outlined" accessibilityRole="button" disabled={!canEdit||busy} onPress={()=>router.push({pathname:"/interchange/import-format",params:{tenant,project:id}})}>{t("interchange_archive_title")}</Button><Button mode="outlined" accessibilityRole="button" onPress={()=>router.push({pathname:"/styles",params:{tenant,project:id}})}>{__uiT('ui_3626e7fdea1c')}</Button><Button mode="outlined" accessibilityRole="button" disabled={!canEdit} onPress={()=>router.push({pathname:"/reviews",params:{tenant}})}>{__uiT('ui_84cb7871b741')}</Button><Button mode="outlined" accessibilityRole="button" onPress={()=>router.push({pathname:"/deliveries",params:{tenant,project:id}})}>{t("delivery_list_title")}</Button><Button mode="outlined" accessibilityRole="button" onPress={()=>router.push({pathname:"/exports",params:{tenant}})}>{__uiT('ui_2f4dcf59ff0c')}</Button><Button mode="outlined" accessibilityRole="button" onPress={()=>router.push({pathname:"/capture",params:{tenant,project:id}})}>{__uiT('ui_ed929c8f2565')}</Button><CollectionCreateWizard client={client} origin={auth.origin} tenant={tenant} project={id} canEdit={canEdit} current={()=>life.current!==null&&!life.current.signal.aborted} onCreated={created=>setItems(old=>[created,...old.filter(item=>item.id!==created.id)])}/>{error?<Text accessibilityRole="alert" style={s.error}>{__uiMessage(error,true)}</Text>:null}</View>} ListEmptyComponent={<Text style={s.empty}>{__uiT('ui_a1c8b6e74da4')}</Text>} renderItem={({item})=><Card style={{marginBottom:8}}><TouchableRipple accessibilityRole="button" onPress={()=>router.push({pathname:"/collections/[id]",params:{id:item.id,tenant}})}><Card.Content><View><Text variant="titleMedium" style={s.name}>{item.name}</Text><Text style={s.meta}>{__r2Value(item.state)}</Text></View><Text style={s.chevron}>›</Text></Card.Content></TouchableRipple></Card>}/></SafeAreaView>}

export default function ProjectDetail(){
 const params=useLocalSearchParams<{id:string;tenant:string}>(),auth=useAuth(),foreground=useReviewForeground();
 const client=useMemo(()=>auth.client(),[auth]),generation=useMemo(()=>crypto.randomUUID(),[client]);
 if(!foreground.active)return null;
 return <ProjectScope key={JSON.stringify([generation,foreground.generation,auth.origin,params.tenant,params.id])}/>;
}
