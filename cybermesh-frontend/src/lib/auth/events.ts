export const AUTH_BOUNDARY_EVENT = "cybermesh:auth-boundary";

export interface AuthBoundaryEventDetail {
  status: 401 | 403;
  path?: string;
  source: "api" | "session";
}

export function dispatchAuthBoundaryEvent(detail: AuthBoundaryEventDetail): void {
  if (typeof window === "undefined") {
    return;
  }
  window.dispatchEvent(new CustomEvent<AuthBoundaryEventDetail>(AUTH_BOUNDARY_EVENT, { detail }));
}
