import { useState, useEffect } from "react";

/**
 * Hook that pauses polling when the browser tab is hidden.
 * Returns an `enabled` flag that becomes false when tab is not visible.
 * This prevents unnecessary API calls when the user isn't looking at the page.
 */
export const useVisibilityPause = (baseEnabled: boolean = true) => {
  const [isVisible, setIsVisible] = useState(() => 
    typeof document !== "undefined" ? document.visibilityState === "visible" : true
  );

  useEffect(() => {
    const handleVisibilityChange = () => {
      setIsVisible(document.visibilityState === "visible");
    };

    document.addEventListener("visibilitychange", handleVisibilityChange);
    return () => {
      document.removeEventListener("visibilitychange", handleVisibilityChange);
    };
  }, []);

  // Combined enabled state: only poll if both base condition and visibility are true
  const enabled = baseEnabled && isVisible;

  return { enabled, isVisible };
};

export default useVisibilityPause;
