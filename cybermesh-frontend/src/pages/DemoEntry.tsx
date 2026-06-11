import { useEffect, useState } from "react";
import { Navigate, useLocation } from "react-router-dom";
import { setDemoMode } from "@/config/demo-mode";
import { ROUTES } from "@/config/routes";

export default function DemoEntry() {
  const location = useLocation();
  const [ready, setReady] = useState(false);

  useEffect(() => {
    setDemoMode(true);
    setReady(true);
  }, []);

  const target = location.state?.from?.pathname || ROUTES.DASHBOARD;

  if (ready) {
    return <Navigate to={target} replace />;
  }

  return null;
}
