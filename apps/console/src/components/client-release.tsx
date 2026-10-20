import {useQuery} from "@tanstack/react-query";
import {api} from "../api";
interface Release {status:string;source:string;observed_at:string;version?:string;url?:string;published_at?:string}
export function ClientRelease({id,repo}:{id:string;repo:string}){
 const q=useQuery({queryKey:["client-release",id,repo],queryFn:()=>api("/clients/"+encodeURIComponent(id)+"/release") as Promise<Release>,staleTime:300000,retry:false,refetchInterval:query=>query.state.data?.status==="pending"?16000:false});
 const r=q.data;
 return <small className="client-release" aria-label="客户端发布版本">{q.isPending?"正在查询版本…":q.isError?"版本未知（查询失败）":r?.status==="current"&&r.version?<a href={r.url} target="_blank" rel="noopener noreferrer" title={"GitHub 发布于 "+r.published_at+"；查询于 "+r.observed_at}>{r.version}</a>:r?.status==="pending"?"版本查询中…":r?.status==="not_found"?"暂无正式发布":r?.status==="rate_limited"?"版本未知（上游限流）":"版本未知"}</small>
}
