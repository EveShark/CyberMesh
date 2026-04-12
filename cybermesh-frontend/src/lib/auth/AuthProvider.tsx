import { createContext, useCallback, useContext, useEffect, useMemo, useState } from "react";
import { getRuntimeConfig, isConfigLoaded } from "@/config/runtime";
import {
  finishHostedLogin,
  getStoredUser,
  getUserManager,
  startHostedLogin,
  startHostedLogout,
} from "@/lib/auth/oidc";
import { fetchBackendSession, updateActiveAccess, type AuthSession } from "@/lib/auth/session";
import { AUTH_BOUNDARY_EVENT, type AuthBoundaryEventDetail, dispatchAuthBoundaryEvent } from "@/lib/auth/events";
import { ROUTES } from "@/config/routes";

interface AuthContextValue {
  authEnabled: boolean;
  isLoading: boolean;
  session: AuthSession | null;
  refreshSession: () => Promise<AuthSession | null>;
  completeHostedLogin: () => Promise<AuthSession>;
  startLogin: (returnTo?: string) => Promise<void>;
  startLogout: () => Promise<void>;
  selectAccess: (accessId: string) => Promise<AuthSession>;
}

const AuthContext = createContext<AuthContextValue | null>(null);
const RETURN_TO_STORAGE_KEY = "cybermesh:return_to";

async function loadSession(): Promise<AuthSession | null> {
  if (!isConfigLoaded() || !getRuntimeConfig().zitadelEnabled) {
    return null;
  }

  const storedUser = await getStoredUser();
  if (!storedUser?.access_token) {
    return null;
  }

  try {
    return await fetchBackendSession(storedUser.access_token);
  } catch (error) {
    if (error instanceof Error && error.message.includes("401")) {
      await getUserManager().removeUser();
      dispatchAuthBoundaryEvent({ status: 401, source: "session" });
      return null;
    }
    throw error;
  }
}

function redirectToBoundaryRoute(status: 401 | 403): void {
  const current = window.location.pathname;
  const authRoutes = new Set([
    ROUTES.AUTH_LOGIN,
    ROUTES.AUTH_CALLBACK,
    ROUTES.AUTH_UNAUTHORIZED,
    ROUTES.AUTH_SESSION_EXPIRED,
  ]);
  if (authRoutes.has(current)) {
    return;
  }
  const target = status === 401 ? ROUTES.AUTH_SESSION_EXPIRED : ROUTES.AUTH_UNAUTHORIZED;
  const next = `${target}?returnTo=${encodeURIComponent(current)}`;
  if (current !== target) {
    window.location.assign(next);
  }
}

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const authEnabled = isConfigLoaded() && getRuntimeConfig().zitadelEnabled;
  const [session, setSession] = useState<AuthSession | null>(null);
  const [isLoading, setIsLoading] = useState<boolean>(authEnabled);

  const refreshSession = useCallback(async () => {
    if (!authEnabled) {
      setSession(null);
      setIsLoading(false);
      return null;
    }

    setIsLoading(true);
    try {
      const nextSession = await loadSession();
      setSession(nextSession);
      return nextSession;
    } finally {
      setIsLoading(false);
    }
  }, [authEnabled]);

  useEffect(() => {
    void refreshSession().catch((error) => {
      console.error("[Auth] Failed to refresh hosted session", error);
      setSession(null);
      setIsLoading(false);
    });
  }, [refreshSession]);

  useEffect(() => {
    if (!authEnabled || !session) {
      return;
    }
    const interval = window.setInterval(() => {
      void refreshSession().catch((error) => {
        console.error("[Auth] Failed to refresh hosted session in background", error);
      });
    }, 60_000);
    return () => window.clearInterval(interval);
  }, [authEnabled, refreshSession, session]);

  useEffect(() => {
    if (!authEnabled) {
      return;
    }
    const refreshOnVisibility = () => {
      if (document.visibilityState !== "visible") {
        return;
      }
      void refreshSession().catch((error) => {
        console.error("[Auth] Failed to refresh hosted session on visibility change", error);
      });
    };
    window.addEventListener("focus", refreshOnVisibility);
    document.addEventListener("visibilitychange", refreshOnVisibility);
    return () => {
      window.removeEventListener("focus", refreshOnVisibility);
      document.removeEventListener("visibilitychange", refreshOnVisibility);
    };
  }, [authEnabled, refreshSession]);

  useEffect(() => {
    if (!authEnabled) {
      return;
    }
    const onBoundary = async (event: Event) => {
      const detail = (event as CustomEvent<AuthBoundaryEventDetail>).detail;
      if (!detail) {
        return;
      }
      if (detail.status === 401) {
        await getUserManager().removeUser();
        setSession(null);
      }
      redirectToBoundaryRoute(detail.status);
    };
    window.addEventListener(AUTH_BOUNDARY_EVENT, onBoundary as EventListener);
    return () => window.removeEventListener(AUTH_BOUNDARY_EVENT, onBoundary as EventListener);
  }, [authEnabled]);

  const completeHostedLogin = useCallback(async () => {
    const user = await finishHostedLogin();
    if (!user?.access_token) {
      throw new Error("Hosted login completed without an access token");
    }
    const nextSession = await fetchBackendSession(user.access_token);
    setSession(nextSession);
    setIsLoading(false);
    return nextSession;
  }, []);

  const startLogin = useCallback(async (returnTo?: string) => {
    if (returnTo && returnTo.startsWith("/")) {
      sessionStorage.setItem(RETURN_TO_STORAGE_KEY, returnTo);
    } else {
      sessionStorage.removeItem(RETURN_TO_STORAGE_KEY);
    }
    await startHostedLogin();
  }, []);

  const startLogout = useCallback(async () => {
    setSession(null);
    await startHostedLogout();
  }, []);

  const selectAccess = useCallback(async (accessId: string) => {
    await updateActiveAccess(accessId);
    const nextSession = await loadSession();
    if (!nextSession) {
      throw new Error("Hosted session is no longer available");
    }
    setSession(nextSession);
    return nextSession;
  }, []);

  const value = useMemo<AuthContextValue>(() => ({
    authEnabled,
    isLoading,
    session,
    refreshSession,
    completeHostedLogin,
    startLogin,
    startLogout,
    selectAccess,
  }), [authEnabled, isLoading, session, refreshSession, completeHostedLogin, startLogin, startLogout, selectAccess]);

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth() {
  const context = useContext(AuthContext);
  if (!context) {
    throw new Error("useAuth must be used within AuthProvider");
  }
  return context;
}
