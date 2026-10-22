import { Readout } from "@migate/ui";
// 指标读数：沿用旧接口，值为“未知”时读数窗显示未知样式。
export function Metric({
  title,
  value,
  caption,
}: {
  title: string;
  value: string;
  caption?: string;
}) {
  return <Readout className="metric" label={title} value={value} caption={caption} />;
}
