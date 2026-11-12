export type Grant = { quota: number; unlimited: boolean; days: number; all_nodes: boolean; node_ids: number[] | null; profile_ids: number[] | null };
export type Plan = { id: number; name: string; enabled: boolean; revision: number; grant: Grant };
export type AccessCode = { id: number; kind: "invite" | "redeem"; plan_id: number | null; plan_revision: number; grant: Grant; expires_at: string | null; used_at: string | null; used_by?: number | null; status: "unused" | "used" | "revoked" | "expired"; synchronization?: "ok" | "pending" | "unknown"; revoked: boolean; created_at: string };

export type CodeKind = AccessCode["kind"];
export type CodePage = { items: AccessCode[]; total: number; page: number };
