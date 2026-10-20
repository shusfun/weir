import {
  Label,Input,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
  Switch,
} from "@migate/ui";
import {PublishedAnnouncement} from "./published-announcement";
import { ResourceSelector } from "./resource-selector";

export function PublicPageFields({
  fields,
  change,
  ownId,
}: {
  fields: Record<string, any>;
  change: (v: Record<string, any>) => void;
  ownId?: number;
}) {
  const themes: Record<string, string> = {
    violet: "紫罗兰",
    ocean: "海蓝",
    forest: "森林",
  };
  return (
    <div className="stack">
      <h3>公告展示</h3>
      <div className="actions"><Switch id="public-notice-enabled" checked={fields.notice_enabled!==false} onCheckedChange={notice_enabled=>change({...fields,notice_enabled})}/><Label htmlFor="public-notice-enabled">显示公开页公告</Label></div>
      <Label htmlFor="public-notice-title">公告标题</Label><Input id="public-notice-title" value={fields.notice_title??""} maxLength={200} placeholder="公告" onChange={e=>change({...fields,notice_title:e.target.value})}/>
      <Label htmlFor="public-notice-type">公告类型</Label><Select value={fields.notice_type||"info"} onValueChange={notice_type=>change({...fields,notice_type})}><SelectTrigger id="public-notice-type"><SelectValue>{({info:"通知",success:"成功",warning:"提醒",error:"重要"} as Record<string,string>)[fields.notice_type||"info"]}</SelectValue></SelectTrigger><SelectContent>{[["info","通知"],["success","成功"],["warning","提醒"],["error","重要"]].map(([v,n])=><SelectItem key={v} value={v}>{n}</SelectItem>)}</SelectContent></Select>
      <div className="actions"><Switch id="public-notice-dismiss" checked={fields.notice_dismissible===true} onCheckedChange={notice_dismissible=>change({...fields,notice_dismissible})}/><Label htmlFor="public-notice-dismiss">允许访客关闭当前公告</Label></div>
      <p className="muted">关闭偏好仅存在当前浏览器；公告内容、标题或类型更新后重新显示。正文按纯文本展示。</p>
      {fields.notice_enabled!==false&&fields.notice&&<div className="announcement-preview"><small>公告预览</small><PublishedAnnouncement preview scope="preview" notice={{notice:fields.notice,notice_title:fields.notice_title,notice_type:fields.notice_type,notice_dismissible:fields.notice_dismissible}}/></div>}
      <Label htmlFor="public-theme">页面主题</Label>
      <Select
        value={fields.theme || "violet"}
        onValueChange={(theme) => change({ ...fields, theme })}
      >
        <SelectTrigger id="public-theme">
          <SelectValue>{themes[fields.theme || "violet"]}</SelectValue>
        </SelectTrigger>
        <SelectContent>
          {Object.entries(themes).map(([key, name]) => (
            <SelectItem key={key} value={key}>
              {name}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <div className="actions">
        <Switch
          id="public-share"
          checked={fields.show_share === true}
          onCheckedChange={(show_share) => change({ ...fields, show_share })}
        />
        <Label htmlFor="public-share">允许复制页面地址与二维码</Label>
      </div>
      <ResourceSelector
        kind="public-page"
        label="目录页面"
        excludeIds={ownId ? [ownId] : []}
        value={fields.page_ids ?? []}
        onChange={(page_ids) => change({ ...fields, page_ids })}
      />
      <p className="muted">
        最多选择 24
        页，可上下调整顺序。目录只显示仍然发布的页面；草稿、已撤下或已删除的页面自动隐藏。不发布节点、订阅令牌或私人订阅组。
      </p>
    </div>
  );
}
