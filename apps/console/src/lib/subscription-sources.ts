import { api } from "../api";
import { useQuery } from "./query";
import type { Source } from "./subscriptions";

export type SourcesResponse = {
  items: Source[];
  traffic: { used: number; total: number; sources: number };
};

// All consumers cache the response envelope; selecting items must not change the cache shape.
export const sourcesQuery = {
  queryKey: ["subscription-sources"],
  queryFn: ({ signal }: { signal: AbortSignal }) => api("/admin/subscriptions/sources", "GET", undefined, { signal }) as Promise<SourcesResponse>,
};

export function useSources(enabled = true) {
  return useQuery({ ...sourcesQuery, enabled });
}
