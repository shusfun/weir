import { api } from "../api";
import { useQuery } from "./query";
import type { Source } from "./subscriptions";

export type SourcesResponse = {
  items: Source[];
  traffic: { used: number; total: number; sources: number };
};

// All consumers cache the response envelope; selecting items must not change the cache shape.
export function useSources() {
  return useQuery({ queryKey: ["subscription-sources"], queryFn: () => api("/admin/subscriptions/sources") as Promise<SourcesResponse> });
}
