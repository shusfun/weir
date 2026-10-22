import {useEffect,useState} from "react";
import {useQuery,useQueryClient} from "@tanstack/react-query";
import { Field, Notice,Button,Card,CardContent,Input,Textarea,Label,Switch,Select,SelectTrigger,SelectValue,SelectContent,SelectItem,Dialog,DialogContent,DialogTitle,DialogDescription,DialogFooter} from "@migate/ui";
import {api} from "../api";
import {PublishedAnnouncement,type NoticeNode,type PublishedNotice} from "./published-announcement";

interface Announcement {content_format?:string;enabled:boolean;authenticated:boolean;public:boolean;title:string;content:string;type:string;dismissible:boolean}
interface Document {announcement:Announcement;revision:string}
const names:Record<string,string>={info:"通知",success:"成功",warning:"提醒",error:"重要"};
const initial:Announcement={enabled:false,authenticated:false,public:false,title:"",content:"",type:"info",dismissible:true};
export function GlobalAnnouncement({audience}:{audience:"authenticated"|"public"}){
 const q=useQuery({queryKey:["global-announcement",audience],queryFn:()=>api(audience==="public"?"/announcements/public":"/announcements") as Promise<{announcement:PublishedNotice|null}>,retry:false,refetchInterval:30000});
 if(q.isPending)return <p className="muted" role="status">正在读取服务公告…</p>;
 if(q.isError)return <Notice role="alert">服务公告暂不可用。<Button variant="ghost" size="sm" onClick={()=>void q.refetch()}>重试公告</Button></Notice>;
 return q.data.announcement?<div className="global-announcement"><PublishedAnnouncement notice={q.data.announcement} scope={"global-"+audience}/></div>:null;
}
export function AnnouncementEditor(){
 const cache=useQueryClient(),q=useQuery({queryKey:["announcement-admin"],queryFn:()=>api("/admin/announcement") as Promise<Document>,refetchOnWindowFocus:false});
 const [draft,D]=useState(initial),[confirm,C]=useState(false),[password,P]=useState(""),[code,CD]=useState(""),[busy,B]=useState(false),[error,E]=useState(""),[notice,N]=useState("");
 useEffect(()=>{if(q.data)D(q.data.announcement)},[q.data?.revision]);
 const preview=useQuery({queryKey:["announcement-markup-preview",draft.content,draft.content_format],queryFn:()=>api("/admin/announcement/preview","POST",{content:draft.content,format:draft.content_format}) as Promise<{nodes:NoticeNode[]}>,enabled:draft.content_format==="html"&&!!draft.content,retry:false,refetchOnWindowFocus:false});
 const dirty=!!q.data&&JSON.stringify(draft)!==JSON.stringify(q.data.announcement);
 function change(p:Partial<Announcement>){D(v=>({...v,...p}));N("")}
 async function save(){if(!q.data)return;B(true);E("");try{await api("/admin/announcement","PUT",{announcement:draft,revision:q.data.revision,password,code});await cache.invalidateQueries({queryKey:["announcement-admin"]});await cache.invalidateQueries({queryKey:["global-announcement"]});C(false);P("");CD("");N(draft.enabled?"公告已发布，仅向选定受众显示。":"公告已撤下，草稿已保留。")}catch(e){E(e instanceof Error?e.message:"公告未能保存，请重试。")}finally{B(false)}}
 return <Card><CardContent className="stack announcement-editor">
  <div><h3>全局公告</h3><p>统一发布服务通知。登录用户与匿名公开页分别选择；页面自己的公告保持独立。</p></div>
  {notice&&<Notice role="status">{notice}</Notice>}{error&&!confirm&&<Notice tone="bad">{error}</Notice>}
  {q.isPending?<p role="status">正在读取公告设置…</p>:q.isError?<Notice tone="bad">公告设置读取失败。<Button onClick={()=>void q.refetch()}>重试读取</Button></Notice>:<>
   <div className="actions"><Switch id="announcement-enabled" checked={draft.enabled} onCheckedChange={enabled=>change({enabled})}/><Label htmlFor="announcement-enabled">发布全局公告</Label></div>
   <div className="form-grid"><div className="stack"><Field label="全局公告标题" htmlFor="announcement-title"><Input id="announcement-title" maxLength={200} value={draft.title} onChange={e=>change({title:e.target.value})} placeholder="服务通知"/></Field></div><div className="stack"><Field label="全局公告类型" htmlFor="announcement-type"><Select value={draft.type} onValueChange={type=>type&&change({type})}><SelectTrigger id="announcement-type"><SelectValue>{names[draft.type]}</SelectValue></SelectTrigger><SelectContent>{Object.entries(names).map(([value,name])=><SelectItem key={value} value={value}>{name}</SelectItem>)}</SelectContent></Select></Field></div></div>
   <Field label="全局公告内容格式" htmlFor="announcement-format"><Select value={draft.content_format||"text"} onValueChange={content_format=>content_format&&change({content_format:content_format==="text"?undefined:content_format})}><SelectTrigger id="announcement-format"><SelectValue>{draft.content_format==="html"?"受限富文本":"纯文本"}</SelectValue></SelectTrigger><SelectContent><SelectItem value="text">纯文本</SelectItem><SelectItem value="html">受限富文本</SelectItem></SelectContent></Select></Field><p className="muted">富文本支持加粗、斜体、段落、换行、列表、代码与 HTTPS 链接；拒绝脚本、样式、图片和事件属性。</p>
   <Field label={draft.content_format==="html"?"全局公告正文（受限富文本）":"全局公告正文（纯文本）"} htmlFor="announcement-content"><Textarea id="announcement-content" rows={6} maxLength={8192} value={draft.content} onChange={e=>change({content:e.target.value})}/></Field>
   <fieldset className="stack"><legend>显示受众</legend><div className="actions"><Switch id="announcement-authenticated" checked={draft.authenticated} onCheckedChange={authenticated=>change({authenticated})}/><Label htmlFor="announcement-authenticated">向登录用户显示</Label></div><div className="actions"><Switch id="announcement-public" checked={draft.public} onCheckedChange={value=>change({public:value})}/><Label htmlFor="announcement-public">向匿名公开页显示</Label></div><p className="muted">匿名公开页任何人均可查看。请勿填写私有订阅、账户信息或密钥。</p></fieldset>
   <div className="actions"><Switch id="announcement-dismissible" checked={draft.dismissible} onCheckedChange={dismissible=>change({dismissible})}/><Label htmlFor="announcement-dismissible">允许关闭全局公告</Label></div>
   <p className="muted">标题、正文或类型更新后重新提示；关闭偏好只保存在当前浏览器。纯文本不执行 HTML；富文本使用服务端白名单结构。</p>
   {draft.content_format==="html"&&preview.isPending&&<p role="status">正在验证富文本…</p>}{draft.content_format==="html"&&preview.isError&&<Notice tone="bad">富文本包含不支持的标签、属性或链接。请修正后再发布。</Notice>}
   {draft.content&&!(draft.content_format==="html"&&(!preview.data||preview.isError))?<div className="announcement-preview"><small>公告预览{!draft.enabled&&" · 草稿未发布"}</small><PublishedAnnouncement preview scope="global-preview" notice={{notice:draft.content,notice_title:draft.title,notice_type:draft.type,notice_dismissible:draft.dismissible,notice_nodes:draft.content_format==="html"?preview.data?.nodes:undefined}}/></div>:<p className="muted">填写正文后在此预览。</p>}
   <div className="actions"><Button disabled={!dirty||busy||draft.content_format==="html"&&(!preview.data||preview.isError)||draft.enabled&&(!draft.content.trim()||!draft.authenticated&&!draft.public)} onClick={()=>{E("");C(true)}}>核对并保存公告</Button>{dirty&&<small>有未保存更改</small>}</div>
  </>}
  <Dialog open={confirm} onOpenChange={v=>{if(!busy){C(v);P("");CD("");E("")}}}><DialogContent><DialogTitle>确认全局公告变更</DialogTitle><DialogDescription>{draft.enabled?"公告将向以下受众显示，请核对内容后验证管理员身份。":"公告将撤下，正文作为草稿保留。"}</DialogDescription><p>{draft.enabled?[draft.authenticated&&"登录用户",draft.public&&"匿名公开页（任何人可见）"].filter(Boolean).join(" / "):"不向任何受众发布"}</p><Field label="管理员当前密码" htmlFor="announcement-password"><Input id="announcement-password" type="password" autoComplete="current-password" value={password} onChange={e=>P(e.target.value)}/></Field><Field label="二次验证码或恢复码" htmlFor="announcement-code"><Input id="announcement-code" autoComplete="one-time-code" value={code} onChange={e=>CD(e.target.value)}/></Field>{error&&<Notice tone="bad">{error}</Notice>}<DialogFooter><Button variant="outline" disabled={busy} onClick={()=>{C(false);P("");CD("");E("")}}>取消</Button><Button disabled={busy||!password} onClick={()=>void save()}>{busy?"正在保存…":"确认保存公告"}</Button></DialogFooter></DialogContent></Dialog>
 </CardContent></Card>;
}
