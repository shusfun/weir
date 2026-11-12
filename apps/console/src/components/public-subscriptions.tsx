import { SubscriptionGuide } from "./subscription-guide";
export interface PublicSubscriptionCard {
  title: string;
  target: string;
  expires_at: number;
  path: string;
}
export function PublicSubscriptions({ items }: { items: PublicSubscriptionCard[] }) {
  return <SubscriptionGuide items={items.map((item) => ({ title: item.title, target: item.target, expiresAt: item.expires_at, url: item.path }))} />;
}
