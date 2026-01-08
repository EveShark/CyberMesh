/**
 * Runtime configuration loader
 * Fetches config from backend API instead of using build-time env vars
 */

interface RuntimeConfig {
  supabaseUrl: string;
  supabaseProjectId: string;
  supabaseKey: string;
  demoMode: boolean;
}

let runtimeConfig: RuntimeConfig | null = null;

/**
 * Load runtime configuration from backend
 * Falls back to build-time env vars if backend is unavailable
 */
export async function loadRuntimeConfig(): Promise<RuntimeConfig> {
  // If already loaded, return cached
  if (runtimeConfig) {
    return runtimeConfig;
  }

  try {
    // Fetch from backend API
    // In development, use relative path for Vite proxy; in production, use full URL
    const backendUrl = import.meta.env.DEV
      ? ''
      : (import.meta.env.VITE_BACKEND_URL || 'https://api.cybermesh.qzz.io:443');
    const response = await fetch(`${backendUrl}/api/v1/frontend-config`, {
      method: 'GET',
      headers: {
        'Content-Type': 'application/json',
      },
      // Short timeout - don't delay page load
      signal: AbortSignal.timeout(3000),
    });

    if (!response.ok) {
      throw new Error(`Config API returned ${response.status}`);
    }

    const data = await response.json();

    runtimeConfig = {
      supabaseUrl: data.supabaseUrl,
      supabaseProjectId: data.supabaseProjectId,
      supabaseKey: data.supabaseKey,
      demoMode: data.demoMode === 'true',
    };

    console.info('[Config] Loaded runtime config from backend', {
      supabaseUrl: runtimeConfig.supabaseUrl,
      demoMode: runtimeConfig.demoMode,
    });

    return runtimeConfig;
  } catch (error) {
    // Fallback to build-time env vars
    console.warn('[Config] Failed to load runtime config, using build-time env vars', error);

    runtimeConfig = {
      supabaseUrl: import.meta.env.VITE_SUPABASE_URL || '',
      supabaseProjectId: import.meta.env.VITE_SUPABASE_PROJECT_ID || '',
      supabaseKey: import.meta.env.VITE_SUPABASE_PUBLISHABLE_KEY || '',
      demoMode: import.meta.env.VITE_DEMO_MODE === 'true',
    };

    return runtimeConfig;
  }
}

/**
 * Get current runtime config (must call loadRuntimeConfig first)
 */
export function getRuntimeConfig(): RuntimeConfig {
  if (!runtimeConfig) {
    throw new Error('Runtime config not loaded. Call loadRuntimeConfig() first.');
  }
  return runtimeConfig;
}

/**
 * Check if runtime config is loaded
 */
export function isConfigLoaded(): boolean {
  return runtimeConfig !== null;
}
