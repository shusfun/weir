import { api } from "../api";
import type { NodeChoice } from "./core";
import { useQuery } from "./query";

export function useNodeChoices(base: string) {
  return useQuery({ queryKey: ["node-choices", base], queryFn: async () => (await api(`${base}/nodes`)).items as (NodeChoice & { gate_id: number })[], refetchInterval: 30_000 });
}
