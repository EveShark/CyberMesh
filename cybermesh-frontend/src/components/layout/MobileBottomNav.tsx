import { NavLink, useLocation } from "react-router-dom";
import { SIDEBAR_ITEMS } from "@/config/navigation";
import { useSidebar } from "@/components/ui/sidebar";

export function MobileBottomNav() {
  const location = useLocation();
  const { setOpenMobile } = useSidebar();
  const currentPath = location.pathname;

  const isActive = (path: string) => currentPath === path;

  const handleNavClick = () => {
    setOpenMobile(false);
    // Scroll to top on navigation
    window.scrollTo(0, 0);
  };

  // Filter out items that should be hidden on mobile
  const mobileItems = SIDEBAR_ITEMS.filter((item) => !item.hideOnMobile);

  return (
    <nav className="fixed bottom-0 left-0 right-0 md:hidden z-50 border-t border-border/30 bg-background/80 backdrop-blur-xl">
      <div className="grid grid-cols-6 h-16 pb-safe">
        {mobileItems.map((item) => {
          const active = isActive(item.url);
          return (
            <NavLink
              key={item.title}
              to={item.url}
              onClick={handleNavClick}
              className={`
                flex flex-col items-center justify-center gap-0.5 
                py-2 transition-all duration-200
                ${active 
                  ? 'text-primary' 
                  : 'text-muted-foreground hover:text-foreground'
                }
              `}
            >
              <div className={`
                relative flex items-center justify-center w-7 h-7 rounded-lg
                transition-all duration-200
                ${active 
                  ? 'bg-accent/15 border border-accent/25' 
                  : ''
                }
              `}>
                <item.icon className={`
                  w-4 h-4 transition-all duration-200
                  ${active 
                    ? 'text-primary' 
                    : ''
                  }
                `} />
              </div>
              <span className={`
                text-[9px] font-medium transition-all duration-200 text-center leading-tight
                ${active ? 'text-primary' : ''}
              `}>
                {item.shortTitle || item.title.split(' ')[0]}
              </span>
            </NavLink>
          );
        })}
      </div>
    </nav>
  );
}

export default MobileBottomNav;
