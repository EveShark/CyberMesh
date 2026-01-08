/**
 * Demo Mode Configuration
 * 
 * When demo mode is enabled, the app shows mock data without making API calls.
 * This is useful for demos, development, and testing.
 * 
 * Priority:
 * 1. localStorage override (set via Settings page)
 * 2. VITE_DEMO_MODE environment variable
 */

const DEMO_MODE_KEY = 'cybermesh-demo-mode';

/**
 * Check if demo mode is enabled
 * Checks localStorage first, then falls back to env var
 */
export const isDemoMode = (): boolean => {
  // Check localStorage first for runtime override
  const storedValue = localStorage.getItem(DEMO_MODE_KEY);
  if (storedValue !== null) {
    return storedValue === 'true';
  }
  
  // Fall back to environment variable
  return import.meta.env.VITE_DEMO_MODE === 'true';
};

/**
 * Set demo mode preference (persisted to localStorage)
 */
export const setDemoMode = (enabled: boolean): void => {
  localStorage.setItem(DEMO_MODE_KEY, enabled ? 'true' : 'false');
};

/**
 * Clear demo mode preference (reverts to env var)
 */
export const clearDemoModePreference = (): void => {
  localStorage.removeItem(DEMO_MODE_KEY);
};

export const getDemoModeLabel = (): string => {
  return 'Demo Mode';
};

export const getDemoModeDescription = (): string => {
  return 'Using sample data for demonstration purposes';
};
