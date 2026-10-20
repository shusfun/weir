import { Card, CardContent } from "@migate/ui";
export function Metric({
  title,
  value,
  caption,
}: {
  title: string;
  value: string;
  caption?: string;
}) {
  return (
    <Card className="metric">
      <CardContent>
        <span className="metric-label">{title}</span>
        <strong className="metric-number">{value}</strong>
        {caption && <small>{caption}</small>}
      </CardContent>
    </Card>
  );
}
