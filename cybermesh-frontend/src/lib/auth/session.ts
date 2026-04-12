import { getRuntimeConfig, isConfigLoaded } from "@/config/runtime";
import { getAccessToken } from "@/lib/auth/oidc";

interface BackendEnvelope<T> {
  success: boolean;
  data: T;
  error?: {
    code?: string;
    message?: string;
  };
}

export interface AuthSession {
  authenticated: boolean;
  auth_source?: string;
  principal_id: string;
  principal_type: string;
  subject?: string;
  preferred_username?: string;
  email?: string;
  allowed_access_ids: string[];
  active_access_id?: string;
  active_delegation?: DelegationRecord;
  capabilities: {
    can_select_access: boolean;
    requires_access_selection: boolean;
    has_active_access: boolean;
    is_global_only: boolean;
    allowed_access_count: number;
    can_manage_delegations: boolean;
    break_glass_enabled: boolean;
  };
}

interface AccessSelectionResponse {
  principal_id: string;
  allowed_access_ids: string[];
  active_access_id?: string;
}

export interface DelegationRecord {
  delegation_id: string;
  principal_id: string;
  principal_type: string;
  status: string;
  approved_by_principal_id?: string;
  approval_reference?: string;
  reason_code: string;
  reason_text: string;
  break_glass: boolean;
  starts_at: string;
  expires_at: string;
  approved_at?: string;
  revoked_at?: string;
  revoked_by_principal_id?: string;
  revoked_reason_code?: string;
  revoked_reason_text?: string;
  access_ids: string[];
}

export interface DelegationListResponse {
  principal_id: string;
  delegations: DelegationRecord[];
  active_grant?: DelegationRecord;
  active_access_id?: string;
}

export interface CreateDelegationInput {
  access_ids: string[];
  reason_code: string;
  reason_text: string;
  starts_at?: string;
  expires_at: string;
  break_glass?: boolean;
  approval_reference?: string;
}

export interface ApproveDelegationInput {
  approval_reference?: string;
}

export interface RevokeDelegationInput {
  reason_code: string;
  reason_text: string;
}

function getBackendUrl(): string {
  return import.meta.env.DEV
    ? ""
    : (import.meta.env.VITE_BACKEND_URL || "https://api.cybermesh.qzz.io:443");
}

async function getRequiredAccessToken(): Promise<string> {
  if (!isConfigLoaded() || !getRuntimeConfig().zitadelEnabled) {
    throw new Error("Hosted authentication is not enabled");
  }

  const accessToken = await getAccessToken();
  if (!accessToken) {
    throw new Error("No active hosted session");
  }
  return accessToken;
}

async function parseEnvelope<T>(response: Response): Promise<T> {
  let body: BackendEnvelope<T> | null = null;
  try {
    body = (await response.json()) as BackendEnvelope<T>;
  } catch {
    body = null;
  }

  if (!response.ok || !body?.success) {
    const detail = body?.error?.message || body?.error?.code;
    throw new Error(detail ? `Backend auth request failed with ${response.status}: ${detail}` : `Backend auth request failed with ${response.status}`);
  }

  return body.data;
}

export async function fetchBackendSession(accessToken?: string): Promise<AuthSession> {
  const token = accessToken ?? await getRequiredAccessToken();
  const response = await fetch(`${getBackendUrl()}/api/v1/auth/me`, {
    method: "GET",
    headers: {
      Authorization: `Bearer ${token}`,
      "Content-Type": "application/json",
    },
    credentials: "include",
  });

  return parseEnvelope<AuthSession>(response);
}

export async function updateActiveAccess(accessId: string): Promise<AccessSelectionResponse> {
  const token = await getRequiredAccessToken();
  const response = await fetch(`${getBackendUrl()}/api/v1/auth/access/select`, {
    method: "POST",
    headers: {
      Authorization: `Bearer ${token}`,
      "Content-Type": "application/json",
    },
    credentials: "include",
    body: JSON.stringify({ access_id: accessId }),
  });

  return parseEnvelope<AccessSelectionResponse>(response);
}

export async function fetchDelegations(scope: "self" | "all" = "self"): Promise<DelegationListResponse> {
  const token = await getRequiredAccessToken();
  const suffix = scope === "all" ? "?scope=all" : "";
  const response = await fetch(`${getBackendUrl()}/api/v1/auth/delegations${suffix}`, {
    method: "GET",
    headers: {
      Authorization: `Bearer ${token}`,
      "Content-Type": "application/json",
    },
    credentials: "include",
  });

  return parseEnvelope<DelegationListResponse>(response);
}

export async function createDelegation(input: CreateDelegationInput): Promise<DelegationRecord> {
  const token = await getRequiredAccessToken();
  const response = await fetch(`${getBackendUrl()}/api/v1/auth/delegations`, {
    method: "POST",
    headers: {
      Authorization: `Bearer ${token}`,
      "Content-Type": "application/json",
    },
    credentials: "include",
    body: JSON.stringify(input),
  });

  return parseEnvelope<DelegationRecord>(response);
}

export async function approveDelegation(delegationId: string, input: ApproveDelegationInput): Promise<DelegationRecord> {
  const token = await getRequiredAccessToken();
  const response = await fetch(`${getBackendUrl()}/api/v1/auth/delegations/${encodeURIComponent(delegationId)}:approve`, {
    method: "POST",
    headers: {
      Authorization: `Bearer ${token}`,
      "Content-Type": "application/json",
    },
    credentials: "include",
    body: JSON.stringify(input),
  });

  return parseEnvelope<DelegationRecord>(response);
}

export async function revokeDelegation(delegationId: string, input: RevokeDelegationInput): Promise<DelegationRecord> {
  const token = await getRequiredAccessToken();
  const response = await fetch(`${getBackendUrl()}/api/v1/auth/delegations/${encodeURIComponent(delegationId)}:revoke`, {
    method: "POST",
    headers: {
      Authorization: `Bearer ${token}`,
      "Content-Type": "application/json",
    },
    credentials: "include",
    body: JSON.stringify(input),
  });

  return parseEnvelope<DelegationRecord>(response);
}
