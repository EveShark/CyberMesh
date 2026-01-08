import { 
  LayoutDashboard, 
  Brain, 
  Activity, 
  AlertTriangle, 
  Blocks, 
  Network,
  Settings,
  type LucideIcon 
} from "lucide-react";
import { ROUTES } from "./routes";

export interface NavItem {
  title: string;
  shortTitle?: string; // For mobile nav
  url: string;
  icon: LucideIcon;
  description?: string;
  hideOnMobile?: boolean;
}

/**
 * Main sidebar navigation items
 */
export const SIDEBAR_ITEMS: NavItem[] = [
  { 
    title: "Command Center",
    shortTitle: "Command",
    url: ROUTES.DASHBOARD, 
    icon: LayoutDashboard,
    description: "Operational dashboard and key metrics"
  },
  { 
    title: "Detection Engine",
    shortTitle: "Detection",
    url: ROUTES.AI_ENGINE, 
    icon: Brain,
    description: "Autonomous threat detection and analysis"
  },
  { 
    title: "Threat Intelligence",
    shortTitle: "Threats",
    url: ROUTES.THREATS, 
    icon: AlertTriangle,
    description: "Threat landscape and incident monitoring"
  },
  { 
    title: "Integrity Ledger",
    shortTitle: "Ledger",
    url: ROUTES.BLOCKCHAIN, 
    icon: Blocks,
    description: "Immutable audit trail and verification"
  },
  { 
    title: "Consensus Network",
    shortTitle: "Network",
    url: ROUTES.NETWORK, 
    icon: Network,
    description: "Distributed validation topology"
  },
  { 
    title: "Infrastructure",
    shortTitle: "Infra",
    url: ROUTES.SYSTEM_HEALTH, 
    icon: Activity,
    description: "Service health and resource monitoring"
  },
  { 
    title: "Settings",
    shortTitle: "Settings",
    url: ROUTES.SETTINGS, 
    icon: Settings,
    description: "Application settings and preferences",
    hideOnMobile: true
  },
];
