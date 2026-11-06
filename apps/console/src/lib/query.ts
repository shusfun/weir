import { useSyncExternalStore } from "react";
import { QueryClient, useQuery as useRawQuery, type Query } from "@tanstack/react-query";
import { isTransient } from "../api";

export const transientGrace = 120_000;
export const queryClient = new QueryClient({defaultOptions:{queries:{retry:1,refetchOnWindowFocus:false,staleTime:15000}}});
type Failure = {since:number;error:unknown;query:Query;timer:ReturnType<typeof setTimeout>};
const failures=new Map<string,Failure>();
const listeners=new Set<()=>void>();
let revision=0;
function emit(){revision++;for(const fn of listeners)fn();}
function subscribe(fn:()=>void){listeners.add(fn);return ()=>{listeners.delete(fn);};}
function snapshot(){return revision;}
queryClient.getQueryCache().subscribe(event=>{
  const q=event.query, hash=q.queryHash;
  if(event.type==="observerAdded" || event.type==="observerRemoved") { emit(); return; }
  if(event.type==="removed" || event.type==="updated" && event.action.type==="success") {
    const previous=failures.get(hash);
    if(previous){clearTimeout(previous.timer);failures.delete(hash);emit();}
  } else if(event.type==="updated" && (event.action.type==="error" || event.action.type==="failed")) {
    const error=q.state.error??q.state.fetchFailureReason;
    const previous=failures.get(hash);
    if(previous){previous.error=error;emit();}
    else {const since=Date.now();failures.set(hash,{since,error,query:q,timer:setTimeout(emit,transientGrace)});emit();}
  }
});
export function useQueryFailures(){
  useSyncExternalStore(subscribe,snapshot,snapshot);
  return [...failures.values()].filter(f=>f.query.getObserversCount()>0);
}
function visibleError(error:unknown,since:number){return !isTransient(error)||Date.now()-since>=transientGrace;}

// Failed refetches retain the data and its mounted content. The shared notice
// reports background failures without replacing the page or its form.
export const useQuery: typeof useRawQuery = ((options:any, client?:QueryClient)=>{
  const interval=options.refetchInterval;
  const result=useRawQuery({...options,refetchInterval:(q:Query)=>isTransient(q.state.error)?5000:typeof interval==="function"?interval(q):interval},client);
  useSyncExternalStore(subscribe,snapshot,snapshot);
  if(!result.error)return result;
  const q=(client??queryClient).getQueryCache().find({queryKey:options.queryKey,exact:true});
  const failure=q?failures.get(q.queryHash):undefined;
  if(result.data!==undefined || !visibleError(result.error,failure?.since??Date.now()))
    return {...result,error:null,isError:false,isLoadingError:false,isRefetchError:false,isPending:result.data===undefined,isLoading:result.data===undefined,status:result.data===undefined?"pending":"success"};
  return result;
}) as typeof useRawQuery;

export function visibleBackgroundFailures(failures:ReturnType<typeof useQueryFailures>) {
  return failures.filter(f=>f.query.state.data!==undefined&&visibleError(f.error,f.since));
}
