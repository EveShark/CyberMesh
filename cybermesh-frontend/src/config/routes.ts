/**
 * Route definitions for CyberMesh application
 */
export const ROUTES = {
  HOME: '/',
  AUTH_LOGIN: '/auth/login',
  AUTH_CALLBACK: '/auth/callback',
  AUTH_UNAUTHORIZED: '/auth/unauthorized',
  AUTH_SESSION_EXPIRED: '/auth/session-expired',
  DASHBOARD: '/dashboard',
  DELEGATIONS: '/delegations',
  AI_ENGINE: '/ai-engine',
  BLOCKCHAIN: '/blockchain',
  NETWORK: '/network',
  THREATS: '/threats',
  SYSTEM_HEALTH: '/system-health',
  SETTINGS: '/settings',
  NOT_FOUND: '/404',
} as const;

export const LANDING_URL =
  (import.meta.env.VITE_LANDING_URL as string | undefined)?.trim() || "/";

export type RouteKey = keyof typeof ROUTES;
export type RoutePath = (typeof ROUTES)[RouteKey];
