import {UserSessions} from "../components/user-sessions";
import { useId, useState, type FormEvent } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { QRCodeSVG } from "qrcode.react";
import { KeyRound, ShieldCheck, Smartphone, LockKeyhole } from "lucide-react";
import { Button, Card, CardContent, Dialog, DialogContent, DialogTitle, DialogDescription, DialogFooter } from "@migate/ui";
import { api } from "../api";
import { accountPasswordError } from "../password";
import { passkey } from "../lib/passkey";
import { date, type Account, type Items } from "../lib/models";
import { Field } from "../components/form-field";
import { State } from "../components/feedback";

type Passkey = {id:number;created_at:string};
type Action = "totp"|"passkey"|"password"|"disable"|"delete-key";
export function SecurityPage({loginRequired}:{loginRequired:()=>void}){
 const id=useId(),cache=useQueryClient();
 const account=useQuery({queryKey:["self-account"],queryFn:()=>api("/me") as Promise<Account>});
 const policy=useQuery({queryKey:["public-policy"],queryFn:()=>api("/public") as Promise<{totp_enabled:boolean;passkey_enabled:boolean}>});

 const keys=useQuery({queryKey:["self-passkeys"],queryFn:()=>api("/security/passkeys") as Promise<Items<Passkey>>});
 const [action,A]=useState<Action|null>(null),[password,P]=useState(""),[code,C]=useState(""),[next,NP]=useState(""),[repeat,RP]=useState("");
 const [busy,B]=useState(false),[error,E]=useState(""),[notice,N]=useState(""),[keyId,KI]=useState<number>(),[relogin,RL]=useState("");
 const [enrollment,T]=useState<{secret?:string;url?:string;recovery?:string[]}>({});
 const titles:Record<Action,string>={totp:"绑定身份验证器",passkey:"绑定 Passkey",password:"修改登录密码",disable:"停用身份验证器","delete-key":"删除 Passkey"};
 function open(v:Action){P("");C("");NP("");RP("");E("");N("");T({});A(v)}
 function close(){if(busy||enrollment.recovery)return;A(null);P("");C("");NP("");RP("");T({});E("")}
 async function submit(e:FormEvent){e.preventDefault();B(true);E("");try{
  if(action==="totp"){
   if(enrollment.url){const result=await api("/security/totp/confirm","POST",{code});T({recovery:result.recovery_codes});P("");C("")}
   else {T(await api("/security/totp/setup","POST",{password,code}));C("")}
  }else if(action==="passkey"){
   await passkey("register",undefined,password,code);await cache.invalidateQueries({queryKey:["self-passkeys"]});A(null);P("");C("");N("Passkey 已绑定")
  }else if(action==="password"){
   if(next!==repeat)throw Error("两次输入的新密码不一致。");const passwordError=accountPasswordError(next);if(passwordError)throw Error(passwordError);await api("/security/password","POST",{password,code,new_password:next});A(null);P("");NP("");RP("");C("");RL("密码已更新，所有旧会话已撤销。")
  }else if(action==="disable"){
   await api("/security/totp/disable","POST",{password,code});A(null);P("");C("");RL("身份验证器已停用，原恢复码和旧会话已失效。")
  }else if(action==="delete-key"){
   await api("/security/passkeys/"+keyId,"DELETE",{password,code});A(null);P("");C("");RL("Passkey 已删除，所有旧会话已撤销。")
  }
 }catch(e){E(e instanceof Error?e.message:"操作失败，请重试。")}finally{B(false)}}
 return <div className="stack security-page"><div className="page-intro"><div><h2>保护你的账户</h2><p>管理登录方式与有效会话。设备配对身份与账户二次验证分别管理。</p></div></div>{notice&&<p className="notice" role="status">{notice}</p>}
 {account.isPending?<State loading/>:account.error?<State error={account.error} onRetry={()=>account.refetch()}/>:<div className="security-method-grid">
  <Card><CardContent><div className="section-heading"><span className="security-method-icon"><LockKeyhole size={21}/></span><span className="tag">已设置</span></div><h3>登录密码</h3><p>修改后撤销所有旧会话，重新登录。</p><Button variant="outline" onClick={()=>open("password")}>修改密码</Button></CardContent></Card>
  <Card><CardContent><div className="section-heading"><span className="security-method-icon"><Smartphone size={21}/></span><span className="tag">{account.data?.totp_enrolled?"已绑定":"未绑定"}</span></div><h3>身份验证器</h3><p>使用动态验证码登录，恢复码仅展示一次。</p><Button variant="outline" disabled={!account.data?.totp_enrolled&&!policy.data?.totp_enabled} onClick={()=>open(account.data?.totp_enrolled?"disable":"totp")}>{account.data?.totp_enrolled?"停用 TOTP":"设置 TOTP"}</Button>{policy.data&&!policy.data.totp_enabled&&!account.data?.totp_enrolled&&<small>管理员尚未开放此方式</small>}</CardContent></Card>
  <Card><CardContent><div className="section-heading"><span className="security-method-icon"><KeyRound size={21}/></span><span className="tag">{keys.isPending?"加载中":keys.error?"未知":`${keys.data?.items?.length??0} 个`}</span></div><h3>Passkey</h3><p>使用设备生物识别或安全密钥完成登录。</p><Button variant="outline" disabled={!policy.data?.passkey_enabled} onClick={()=>open("passkey")}>绑定 Passkey</Button>{policy.data&&!policy.data.passkey_enabled&&<small>管理员尚未开放此方式</small>}</CardContent></Card>
 </div>}
 {policy.error&&<State error={policy.error} onRetry={()=>policy.refetch()}/>}
 <Card><CardContent><div className="section-heading"><div><h2>已绑定的 Passkey</h2><p>删除需要验证身份，并撤销全部会话。</p></div></div>{keys.isPending?<State loading/>:keys.error?<State error={keys.error} onRetry={()=>keys.refetch()}/>:!keys.data?.items?.length?<p className="security-inline-empty">尚未绑定 Passkey，可添加此设备或安全密钥。</p>:keys.data.items.map((key,i)=><div className="list-row" key={key.id}><div><strong>Passkey {i+1}</strong><small>绑定于 {date(key.created_at)}</small></div><Button variant="outline" size="sm" onClick={()=>{KI(key.id);open("delete-key")}}>删除 Passkey {i+1}</Button></div>)}</CardContent></Card>
 <UserSessions onCurrentRevoked={()=>RL("当前会话已撤销，请重新登录。")}/>
 <Dialog open={!!action} onOpenChange={v=>!v&&close()}><DialogContent className="security-dialog"><DialogTitle>{action?titles[action]:"账户安全"}</DialogTitle><DialogDescription>{enrollment.recovery?"请离线保存恢复码。关闭本页后无法再次查看。":"操作仅影响你的账户。敏感变更需验证当前密码与已绑定的二次验证。"}</DialogDescription>
 {enrollment.recovery?<div className="stack"><div className="recovery-codes sensitive-output">{enrollment.recovery.map(value=><code key={value}>{value}</code>)}</div><p>每个恢复码只能使用一次；不要发送给其他人。</p><Button onClick={()=>{T({});A(null);loginRequired()}}>已保存恢复码，重新登录</Button></div>:<form className="stack" onSubmit={submit}>
 {enrollment.url?<><div className="totp-setup sensitive-output"><QRCodeSVG value={enrollment.url} size={180}/><div><strong>扫描二维码</strong><p>在身份验证器中添加账户，或手动输入密钥。</p><code>{enrollment.secret}</code></div></div><Field name={id+"confirm-code"} label="身份验证器动态码" value={code} onChange={C} autoComplete="one-time-code"/></>:<><Field name="security-password" label="当前密码" type="password" value={password} onChange={P} autoComplete="current-password"/>{account.data?.totp_enrolled&&<Field name={id+"code"} label="二次验证码 / 恢复码" value={code} onChange={C} autoComplete="one-time-code"/>}{action==="password"&&<><Field name={id+"new"} label="新密码（至少 6 位，含大小写字母和数字）" type="password" value={next} onChange={NP}/><Field name={id+"repeat"} label="再次输入新密码" type="password" value={repeat} onChange={RP}/></>}</>}
 {error&&<p role="alert" className="error-message">{error}</p>}<DialogFooter><Button type="button" variant="outline" disabled={busy} onClick={close}>取消</Button><Button type="submit" disabled={busy}>{busy?"正在处理…":enrollment.url?"确认 TOTP":action==="totp"?"生成绑定二维码":action==="passkey"?"继续绑定 Passkey":action==="password"?"确认修改密码":action==="disable"?"确认停用 TOTP":"确认删除 Passkey"}</Button></DialogFooter></form>}
 </DialogContent></Dialog>

 <Dialog open={!!relogin}><DialogContent><DialogTitle>请重新登录</DialogTitle><DialogDescription>{relogin}</DialogDescription><Button onClick={()=>{cache.clear();loginRequired()}}>返回登录</Button></DialogContent></Dialog>
 </div>;
}
