import {createElement,useEffect,useState,type ReactNode} from "react";
import {Bell,X} from "lucide-react";
import { Notice,Button,Card,CardContent,SectionHeader} from "@migate/ui";
export interface NoticeNode {tag?:string;text?:string;href?:string;children?:NoticeNode[]}
export interface PublishedNotice {notice:string;notice_title?:string;notice_type?:string;notice_dismissible?:boolean;notice_revision?:string;notice_nodes?:NoticeNode[]}
const tags=new Set(["b","i","em","strong","a","p","br","ul","li","code","pre"]);
function renderNodes(nodes:NoticeNode[],depth=0):ReactNode {
 if(depth>12||nodes.length>256)return null;
 return nodes.map((node,i)=>{if(!node.tag)return node.text??"";if(!tags.has(node.tag))return null;let href:string|undefined;
  if(node.tag==="a"&&node.href){try{const u=new URL(node.href);if(u.protocol==="https:"&&!u.username&&!u.password)href=u.href}catch{}}
  return createElement(node.tag,{key:i,...(href?{href,target:"_blank",rel:"noopener noreferrer"}:{})},node.tag==="br"?undefined:renderNodes(node.children??[],depth+1));});
}
export function PublishedAnnouncement({notice,scope,preview=false}:{notice:PublishedNotice;scope:string;preview?:boolean}){
 const key="migate:notice:"+scope+":"+(notice.notice_revision??"draft");
 const [hidden,H]=useState(false),[error,E]=useState("");
 useEffect(()=>{E("");if(preview){H(false);return}try{H(notice.notice_dismissible===true&&localStorage.getItem(key)==="dismissed")}catch{H(false);E("浏览器无法读取公告偏好；公告仍会显示。")}},[key,notice.notice_dismissible,preview]);
 if(!notice.notice||hidden)return null;
 function dismiss(){if(preview)return;H(true);try{localStorage.setItem(key,"dismissed")}catch{E("浏览器未保存偏好，公告保持显示。请允许站点存储后重试。");H(false)}}
 return <Card size="lg" className="published-announcement" data-notice-type={notice.notice_type||"info"}><CardContent>
  <SectionHeader align="start" className="mb-2.5" title={<span className="published-section-title"><Bell size={18}/>{notice.notice_title||"公告"}</span>} actions={notice.notice_dismissible&&<Button variant="ghost" size="icon-sm" aria-label="关闭公告" disabled={preview} onClick={dismiss}><X size={16}/></Button>}/>
  {notice.notice_nodes?<div className="published-notice-content notice-rich-content">{renderNodes(notice.notice_nodes)}</div>:<p className="published-notice-content">{notice.notice}</p>}{error&&<Notice tone="bad">{error}</Notice>}
 </CardContent></Card>
}
