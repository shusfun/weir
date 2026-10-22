import {useEffect,useRef,useState} from "react";
import {useQuery,useQueryClient} from "@tanstack/react-query";
import { ReorderButtons, Field, Notice, SearchInput, SectionHeader,Button,Card,CardContent,Input,Textarea,Label,Switch,Checkbox,Select,SelectTrigger,SelectValue,SelectContent,SelectItem,Dialog,DialogContent,DialogTitle,DialogDescription,DialogFooter} from "@migate/ui";
import {Plus} from "lucide-react";
import {ClientIcon} from "./client-icon";
import {client} from "../lib/client";
import {api} from "../api";
import type {CatalogClient} from "../lib/models";
const platforms=["windows","macos","linux","android","ios","harmonyos"];
const platformNames:Record<string,string>={windows:"Windows",macos:"macOS",linux:"Linux",android:"Android",ios:"iOS",harmonyos:"HarmonyOS"};
const iconNames:Record<string,string>={monitor:"桌面",phone:"手机",globe:"通用",shield:"安全"};
const formats=[["base64","通用订阅"],["clash","Clash / Mihomo"],["singbox","sing-box"],["surge","Surge"],["loon","Loon"],["quanx","Quantumult X"],["egern","Egern"]];
export function ClientCatalogEditor(){
 const cache=useQueryClient();const q=useQuery({queryKey:["client-catalog-admin"],queryFn:()=>client.catalog(true),refetchOnWindowFocus:false});
 const [search,S]=useState("");
 const [items,I]=useState<CatalogClient[]>([]),[editor,E]=useState<CatalogClient|null>(null),[removing,RM]=useState<string|null>(null),[confirm,C]=useState(false),[reset,RS]=useState(false),[password,P]=useState(""),[code,CD]=useState(""),[busy,B]=useState(false),[error,ERR]=useState(""),[notice,N]=useState("");
 const uploadGeneration=useRef(0);
 useEffect(()=>{uploadGeneration.current++},[editor?.id]);
 useEffect(()=>{if(q.data)I(q.data.clients)},[q.data?.revision]);
 function closeConfirmation(){if(!busy){C(false);P("");CD("");ERR("")}}
 const dirty=!!q.data&&JSON.stringify(items)!==JSON.stringify(q.data.clients);
 const set=(key:keyof CatalogClient,value:any)=>editor&&E({...editor,[key]:value});
 function move(index:number,delta:number){const next=[...items];[next[index],next[index+delta]]=[next[index+delta],next[index]];I(next);N("")}
 async function uploadIcon(file:File|undefined){if(!file||!editor)return;const generation=++uploadGeneration.current,id=editor.id;ERR("");if(!["image/png","image/jpeg"].includes(file.type)||file.size>200*1024){ERR("请选择不超过 200 KiB 的 PNG / JPEG 图片，边长最多 512 像素。");return}try{const data=await new Promise<string>((resolve,reject)=>{const reader=new FileReader();reader.onerror=()=>reject(Error("图片读取失败"));reader.onload=()=>resolve(String(reader.result));reader.readAsDataURL(file)});if(generation===uploadGeneration.current)E(current=>current?.id===id?{...current,icon_data:data}:current)}catch{if(generation===uploadGeneration.current)ERR("图片读取失败，请重新选择。")}}
 async function save(){if(!q.data)return;B(true);ERR("");try{await api("/admin/clients","PUT",{clients:items,revision:q.data.revision,password,code,reset});await cache.invalidateQueries({queryKey:["client-catalog-admin"]});await cache.invalidateQueries({queryKey:["client-catalog"]});C(false);RS(false);P("");CD("");N("客户端目录已保存；用户仅显示启用项。")}catch(e){ERR(e instanceof Error?e.message:"保存失败，请重试")}finally{B(false)}}
 return <Card><CardContent className="stack client-catalog-editor">
  <SectionHeader level="h3" title="客户端目录" description="下载入口、平台、格式与显示顺序。变更核对后验证管理员身份保存。" actions={<Button onClick={()=>{ERR("");E({id:"client-"+crypto.randomUUID().slice(0,8),name:"",description:"",platforms:["windows"],url:"",repo:"",icon:"monitor",format:"clash",import:"copy",enabled:true})}}><Plus size={15}/>新增客户端</Button>} />
  {notice&&<Notice role="status">{notice}</Notice>}{error&&<Notice tone="bad">{error}</Notice>}
  {q.isPending?<p role="status">正在读取目录…</p>:q.isError?<Notice tone="bad">客户端目录读取失败。<Button onClick={()=>void q.refetch()}>重试读取</Button></Notice>:<>
   <SearchInput aria-label="搜索客户端目录" value={search} onChange={e=>S(e.target.value)} placeholder="搜索名称、平台或说明"/>
   {!items.length&&<p>目录为空；用户仍可复制已有格式订阅。</p>}
   {items.map((v,index)=>({v,index})).filter(({v})=>[v.name,v.description,...v.platforms.map(p=>platformNames[p]??p)].join(" ").toLocaleLowerCase().includes(search.toLocaleLowerCase())).map(({v,index})=><article key={v.id} className="list-row catalog-row"><div className="catalog-identity"><ClientIcon icon={v.icon} data={v.icon_data}/><div><strong>{v.name}</strong><small>{v.platforms.map(p=>platformNames[p]??p).join(" / ")} · {formats.find(([f])=>f===v.format)?.[1]??v.format} · {v.enabled?"已启用":"已停用"}</small><small>{v.description}</small></div></div><div className="actions"><ReorderButtons label={v.name} onUp={()=>move(index,-1)} onDown={()=>move(index,1)} upDisabled={index===0} downDisabled={index===items.length-1} /><Button variant="outline" size="sm" onClick={()=>E({...v,platforms:[...v.platforms]})}>编辑</Button><Button variant="destructive" size="sm" onClick={()=>RM(v.id)}>删除</Button></div></article>)}
   {!!search&&!items.some(v=>[v.name,v.description,...v.platforms.map(p=>platformNames[p]??p)].join(" ").toLocaleLowerCase().includes(search.toLocaleLowerCase()))&&<p role="status">没有匹配的客户端。</p>}
   <div className="actions"><Button variant="outline" onClick={()=>{RS(true);C(true);ERR("")}}>恢复默认目录</Button><Button disabled={!dirty} onClick={()=>{RS(false);C(true);ERR("")}}>核对并保存目录</Button>{dirty&&<small>有未保存更改，顺序尚未应用</small>}</div>
  </>}
  <Dialog open={!!editor} onOpenChange={v=>!v&&E(null)}><DialogContent size="lg"><DialogTitle>{items.some(v=>v.id===editor?.id)?"编辑客户端":"新增客户端"}</DialogTitle><DialogDescription>仅 HTTPS 下载地址；不接受令牌模板或自定义执行协议。图标使用统一组件。</DialogDescription>{editor&&<form className="stack" onSubmit={e=>{e.preventDefault();const next=items.some(v=>v.id===editor.id)?items.map(v=>v.id===editor.id?editor:v):[...items,editor];I(next);E(null);N("")}}>
   <Field label="客户端名称" htmlFor="catalog-name"><Input id="catalog-name" value={editor.name} required maxLength={120} onChange={e=>set("name",e.target.value)}/></Field>
   <Field label="客户端说明" htmlFor="catalog-description"><Textarea id="catalog-description" value={editor.description} maxLength={600} onChange={e=>set("description",e.target.value)}/></Field>
   <Field label="客户端下载地址" htmlFor="catalog-url"><Input id="catalog-url" value={editor.url} type="url" required onChange={e=>set("url",e.target.value)}/></Field>
   <Field label="GitHub 仓库（owner/repo，可选）" htmlFor="catalog-repo"><Input id="catalog-repo" value={editor.repo} onChange={e=>set("repo",e.target.value)}/></Field>
   <fieldset className="resource-selector"><legend>适用平台</legend><div className="actions">{platforms.map(p=><Label key={p}><Checkbox checked={editor.platforms.includes(p)} onCheckedChange={v=>set("platforms",v?[...editor.platforms,p]:editor.platforms.filter(x=>x!==p))}/>{platformNames[p]}</Label>)}</div></fieldset>
   <Field label="订阅格式"><Select value={editor.format} onValueChange={v=>{if(v)E({...editor,format:v,import:v==="clash"?editor.import:"copy"})}}><SelectTrigger aria-label="目录订阅格式"><SelectValue>{formats.find(([v])=>v===editor.format)?.[1]??editor.format}</SelectValue></SelectTrigger><SelectContent>{formats.map(([v,n])=><SelectItem key={v} value={v}>{n}</SelectItem>)}</SelectContent></Select></Field>
   <Field label="目录图标"><Select value={editor.icon} onValueChange={v=>v&&set("icon",v)}><SelectTrigger aria-label="目录图标"><SelectValue>{iconNames[editor.icon]??editor.icon}</SelectValue></SelectTrigger><SelectContent>{[["monitor","桌面"],["phone","手机"],["globe","通用"],["shield","安全"]].map(([v,n])=><SelectItem key={v} value={v}>{n}</SelectItem>)}</SelectContent></Select></Field>
   <Field label="上传客户端图标（可选）" htmlFor="catalog-icon-upload"><Input id="catalog-icon-upload" type="file" accept="image/png,image/jpeg" onChange={e=>{void uploadIcon(e.target.files?.[0]);e.target.value=""}}/></Field><p className="muted">PNG / JPEG ≤ 200 KiB、边长 ≤ 512 像素；发布时由服务器重新编码。留空使用统一图标，整个目录上限 768 KiB。</p>{editor.icon_data&&<div className="actions"><ClientIcon icon={editor.icon} data={editor.icon_data}/><Button type="button" variant="outline" onClick={()=>{uploadGeneration.current++;set("icon_data","")}}>移除上传图标</Button></div>}{error&&<Notice tone="bad">{error}</Notice>}
   {editor.format==="clash"&&<Label><Switch checked={editor.import==="clash"} onCheckedChange={v=>set("import",v?"clash":"copy")}/>允许 Clash 协议导入</Label>}
   <Label><Switch checked={editor.enabled} onCheckedChange={v=>set("enabled",v)}/>向用户显示此客户端</Label>
   <DialogFooter><Button type="button" variant="outline" onClick={()=>E(null)}>取消</Button><Button type="submit" disabled={!editor.platforms.length}>暂存客户端</Button></DialogFooter>
  </form>}</DialogContent></Dialog>
  <Dialog open={!!removing} onOpenChange={v=>!v&&RM(null)}><DialogContent><DialogTitle>移除客户端目录项</DialogTitle><DialogDescription>仅从目录移除，不删除用户授权或订阅。保存目录后生效。</DialogDescription><DialogFooter><Button variant="outline" onClick={()=>RM(null)}>取消</Button><Button variant="destructive" onClick={()=>{I(items.filter(v=>v.id!==removing));RM(null)}}>确认移除目录项</Button></DialogFooter></DialogContent></Dialog>
  <Dialog open={confirm} onOpenChange={v=>!v&&closeConfirmation()}><DialogContent><DialogTitle>{reset?"恢复默认客户端目录":"确认保存客户端目录"}</DialogTitle><DialogDescription>{reset?"覆盖当前目录及顺序，用户授权保持。":"将发布 "+items.length+" 项客户端及当前顺序。"}下载入口不接收订阅令牌。</DialogDescription><form className="stack" onSubmit={e=>{e.preventDefault();void save()}}><Field label="管理员密码" htmlFor="catalog-admin-password"><Input id="catalog-admin-password" type="password" value={password} onChange={e=>P(e.target.value)} required autoComplete="current-password"/></Field><Field label="二次验证码（已绑定时必填）" htmlFor="catalog-admin-code"><Input id="catalog-admin-code" value={code} inputMode="numeric" onChange={e=>CD(e.target.value)} autoComplete="one-time-code"/></Field>{error&&<Notice tone="bad">{error}</Notice>}<DialogFooter><Button type="button" variant="outline" disabled={busy} onClick={closeConfirmation}>取消</Button><Button type="submit" disabled={busy}>{busy?"保存中…":"确认发布目录"}</Button></DialogFooter></form></DialogContent></Dialog>
 </CardContent></Card>
}
