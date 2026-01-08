import { useLocation, Link } from "react-router-dom";
import { ChevronRight, Home } from "lucide-react";
import { SIDEBAR_ITEMS } from "@/config/navigation";
import { cn } from "@/lib/utils";

const Breadcrumbs = () => {
  const location = useLocation();
  const currentPath = location.pathname;

  // Find the current nav item based on path
  const currentNavItem = SIDEBAR_ITEMS.find(item => item.url === currentPath);

  // Don't show breadcrumbs if we can't identify the current page
  if (!currentNavItem) return null;

  return (
    <nav 
      aria-label="Breadcrumb" 
      className="px-4 sm:px-6 lg:px-8 py-3 border-b border-border/30 bg-background/50 backdrop-blur-sm"
    >
      <ol className="flex items-center gap-1.5 text-xs sm:text-sm">
        {/* Home link */}
        <li>
          <Link 
            to="/" 
            className="flex items-center gap-1 text-muted-foreground hover:text-foreground transition-colors"
          >
            <Home className="w-3.5 h-3.5" />
            <span className="hidden sm:inline">Home</span>
          </Link>
        </li>

        {/* Separator */}
        <li>
          <ChevronRight className="w-3.5 h-3.5 text-muted-foreground/50" />
        </li>

        {/* Dashboard link */}
        <li>
          <Link 
            to="/dashboard" 
            className={cn(
              "transition-colors",
              currentPath === "/dashboard" 
                ? "text-foreground font-medium" 
                : "text-muted-foreground hover:text-foreground"
            )}
          >
            Dashboard
          </Link>
        </li>

        {/* Current page (if not dashboard) */}
        {currentPath !== "/dashboard" && (
          <>
            <li>
              <ChevronRight className="w-3.5 h-3.5 text-muted-foreground/50" />
            </li>
            <li className="flex items-center gap-1.5">
              <currentNavItem.icon className="w-3.5 h-3.5 text-frost" />
              <span className="text-foreground font-medium">
                {currentNavItem.title}
              </span>
            </li>
          </>
        )}
      </ol>
    </nav>
  );
};

export default Breadcrumbs;
