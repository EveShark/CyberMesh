import { UserManager, WebStorageStateStore } from "oidc-client-ts";
import { getRuntimeConfig } from "@/config/runtime";

const storageKeyPrefix = "cybermesh-zitadel";

function ensureAuthEnabled() {
  const config = getRuntimeConfig();
  if (!config.zitadelEnabled || !config.zitadelIssuer || !config.zitadelClientId) {
    throw new Error("ZITADEL hosted login is not configured");
  }
  return config;
}

function getAuthorityConfig() {
  const config = ensureAuthEnabled();
  const origin = window.location.origin;
  return {
    authority: config.zitadelIssuer,
    client_id: config.zitadelClientId,
    redirect_uri: `${origin}/auth/callback`,
    post_logout_redirect_uri: `${origin}/`,
    response_type: "code",
    scope: "openid profile email",
  };
}

let userManager: UserManager | null = null;

export function getUserManager(): UserManager {
  if (userManager) {
    return userManager;
  }
  const authority = getAuthorityConfig();
  userManager = new UserManager({
    ...authority,
    userStore: new WebStorageStateStore({ store: window.localStorage, prefix: storageKeyPrefix }),
  });
  return userManager;
}

export async function startHostedLogin(): Promise<void> {
  await getUserManager().signinRedirect();
}

export async function finishHostedLogin() {
  return getUserManager().signinCallback();
}

export async function startHostedLogout(): Promise<void> {
  await getUserManager().signoutRedirect();
}

export async function getStoredUser() {
  return getUserManager().getUser();
}

export async function getAccessToken(): Promise<string | null> {
  const user = await getStoredUser();
  return user?.access_token ?? null;
}
