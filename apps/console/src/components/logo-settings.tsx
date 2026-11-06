import { useState } from "react";
import { Button, Field, FilePicker } from "@weir/ui";
import { Trash2 } from "lucide-react";
import { api } from "../api";
import { useRowAction } from "../lib/action";
import { useLogo } from "./brand-mark";
import { State } from "./feedback";
import { Section } from "./system-settings";

export function LogoSettings() {
  const logo = useLogo();
  const [error, setError] = useState("");
  const { run, isBusy } = useRowAction({ invalidate: [["logo"]] });
  async function upload(file: File) {
    setError("");
    try {
      const data = await new Promise<string>((resolve, reject) => {
        const reader = new FileReader();
        reader.onload = () => resolve(String(reader.result));
        reader.onerror = () => reject(new Error("图片读取失败"));
        reader.readAsDataURL(file);
      });
      await run("upload", () => api("/admin/settings/logo", "PUT", { data }), "Logo 已保存");
    } catch (e) { setError(e instanceof Error ? e.message : "图片读取失败"); }
  }
  return <Section title="Logo" description="登录页和控制台使用的标识。">
    {logo.isPending || logo.error || !logo.data ? <State loading={logo.isPending} error={logo.error} onRetry={() => logo.refetch()} /> : <div className="stack">
      {logo.data.data ? <img src={logo.data.data} alt="当前 Logo" width={64} height={64} className="object-contain" /> : <p>尚未设置 Logo</p>}
      <Field label="上传 Logo" error={error} hint="PNG、JPEG 或 GIF，最大 512 KiB，边长不超过 1024 像素。">
        <FilePicker accept="image/png,image/jpeg,image/gif" maxBytes={512 * 1024} disabled={isBusy()} onError={setError} onFile={(file) => void upload(file)}>选择图片，或拖到这里</FilePicker>
      </Field>
      {logo.data.data && <div><Button variant="outline" disabled={isBusy()} busy={isBusy("remove")} onClick={() => void run("remove", () => api("/admin/settings/logo", "DELETE"), "Logo 已移除")}><Trash2 />移除 Logo</Button></div>}
    </div>}
  </Section>;
}
