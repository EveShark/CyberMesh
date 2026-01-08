/**
 * Route definitions for CyberMesh application
 */
export const ROUTES = {
  HOME: '/',
  DASHBOARD: '/dashboard',
  AI_ENGINE: '/ai-engine',
  BLOCKCHAIN: '/blockchain',
  NETWORK: '/network',
  THREATS: '/threats',
  SYSTEM_HEALTH: '/system-health',
  SETTINGS: '/settings',
  NOT_FOUND: '/404',
} as const;

export type RouteKey = keyof typeof ROUTES;
export type RoutePath = (typeof ROUTES)[RouteKey];
