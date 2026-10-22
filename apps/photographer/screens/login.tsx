import {ActivityIndicator, Button, Card, Text, TextInput} from "react-native-paper";
import {LoginAttempts} from "../src/login-attempts";
import {useLocale} from "../src/locale-context";
import { useEffect, useMemo, useRef, useState } from "react";
import { router } from '../src/localized-router';
import {SafeAreaView, StyleSheet, View} from "react-native";
import { useAuth } from "../src/auth-context";

type Step="email"|"code";
export default function Login({registrationMode=false}:{registrationMode?:boolean}={}){
 const __uiMessage=useLocale().message;

 const auth=useAuth(),{t,locale}=useLocale(),client=useMemo(()=>auth.client(),[auth]);
 const latestClient=useRef(client);latestClient.current=client;const latestGeneration=useRef(auth.sessionGeneration());latestGeneration.current=auth.sessionGeneration();
 const flight=useRef<AbortController|null>(null),mounted=useRef(true),scope=useRef({origin:auth.origin,clientType:auth.clientType}),deadline=useRef<ReturnType<typeof setTimeout>|undefined>(undefined),latestScope=useRef('');latestScope.current=JSON.stringify([auth.origin,auth.clientType]);
 const [initialAttempts]=useState(()=>new LoginAttempts(auth.clientType));const attempts=useRef(initialAttempts);
 const [email,setEmail]=useState(''),[code,setCode]=useState(''),[challenge,setChallenge]=useState(''),[step,setStep]=useState<Step>('email'),[busy,setBusy]=useState(false),[error,setError]=useState('');
 useEffect(()=>{mounted.current=true;return()=>{mounted.current=false;flight.current?.abort();clearTimeout(deadline.current);};},[]);
 useEffect(()=>{if(scope.current.origin!==auth.origin||scope.current.clientType!==auth.clientType){flight.current?.abort();clearTimeout(deadline.current);flight.current=null;scope.current={origin:auth.origin,clientType:auth.clientType};attempts.current=new LoginAttempts(auth.clientType);setBusy(false);setChallenge('');setCode('');setStep('email');}if(auth.principal()&&!flight.current)router.replace('/workspaces');},[auth,busy]);
 async function submit(){
  if(flight.current||!email||(step==='code'&&!code))return;
  const controller=new AbortController();flight.current=controller;const expected=scope.current;let accepting=false;
  const current=()=>mounted.current&&!controller.signal.aborted&&flight.current===controller&&scope.current===expected&&(accepting||latestClient.current===client)&&latestScope.current===JSON.stringify([expected.origin,expected.clientType]);
  setBusy(true);setError('');
  const timeout=setTimeout(()=>{if(current()){controller.abort();flight.current=null;setBusy(false);setError(t('read_error'));}},15000);deadline.current=timeout;
  try{
   const headers=await attempts.current.headers(signal=>client.request<{csrf_token:string}>('/api/v1/auth/csrf',{signal}),controller.signal);if(!current())return;
   if(step==='email'){
    const attempt=attempts.current.requestAttempt(email,locale);
    const out=await client.request<{challenge_id:string}>('/api/v1/auth/otp/request',{method:'POST',signal:controller.signal,headers,body:attempt.body,idempotencyKey:attempt.key});
    if(!current())return;setChallenge(out.challenge_id);setStep('code');
   }else{
    const attempt=attempts.current.verifyAttempt(challenge,code);
    const out=await client.request<{access_token:string;refresh_token?:string;csrf_token:string}>('/api/v1/auth/otp/verify',{method:'POST',signal:controller.signal,headers,body:attempt.body,idempotencyKey:attempt.key});
    if(!current())return;accepting=true;const epoch=await auth.acceptLoginTokens(out,controller.signal);if(mounted.current&&!controller.signal.aborted&&flight.current===controller&&scope.current===expected&&latestGeneration.current===epoch)router.replace('/workspaces');
   }
  }catch{if(current())setError(t('read_error'));}
  finally{clearTimeout(timeout);if(flight.current===controller){flight.current=null;if(mounted.current)setBusy(false);}}
 }
 function cancel(){flight.current?.abort();clearTimeout(deadline.current);flight.current=null;setBusy(false);setError(t('read_cancelled'));}
 return <SafeAreaView style={s.root}><Card style={s.card}><Card.Content><Button mode="text" disabled={busy} onPress={()=>router.push(registrationMode?'/login':'/register')}>{registrationMode?t('login_sign_in'):t('registration_title')}</Button><Text accessibilityRole="header" style={s.title}>{registrationMode?t('registration_title'):t('login_title')}</Text><Text>{registrationMode?t('registration_help'):t('login_help')}</Text><Text style={s.label}>{step==='email'?t('login_email'):t('login_code')}</Text><TextInput mode="outlined" editable={!busy} autoCapitalize="none" autoComplete={step==='email'?'email':'one-time-code'} keyboardType={step==='email'?'email-address':'number-pad'} value={step==='email'?email:code} onChangeText={step==='email'?setEmail:setCode} style={s.input} accessibilityLabel={step==='email'?t('login_email'):t('login_code')}/>{error?<Text accessibilityRole="alert" style={s.error}>{__uiMessage(error,true)}</Text>:null}<Button mode="contained" loading={busy} accessibilityRole="button" disabled={busy||!email||step==='code'&&!code} onPress={()=>void submit()}>{step==='email'?t('login_continue'):t('login_sign_in')}</Button>{busy?<Button mode="outlined" accessibilityRole="button" onPress={cancel}>{t('read_cancel')}</Button>:null}{(step==='code'||!!error)&&!busy?<Button mode="outlined" accessibilityRole="button" onPress={()=>{attempts.current.newChallenge();setStep('email');setChallenge('');setCode('');setError('');}}>{t('login_request_new')}</Button>:null}</Card.Content></Card></SafeAreaView>;
}
const s=StyleSheet.create({root:{flex:1,backgroundColor:"#222",justifyContent:"center",padding:24},card:{maxWidth:440,width:"100%",alignSelf:"center",backgroundColor:"#303030",padding:24,borderRadius:16},title:{fontSize:30,fontWeight:"700",color:"white",marginBottom:28},label:{color:"#ddd",marginBottom:8},input:{minHeight:48,borderWidth:1,borderColor:"#777",borderRadius:8,color:"white",paddingHorizontal:12,fontSize:18},error:{color:"#ffb4ab",marginTop:12},button:{minHeight:48,marginTop:20,borderRadius:8,backgroundColor:"#d0bcff",alignItems:"center",justifyContent:"center"},buttonText:{color:"#201a2b",fontWeight:"700",fontSize:16}});
