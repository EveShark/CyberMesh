import { Link } from "react-router-dom";
import { Button } from "@/components/ui/button";
import Logo from "@/components/brand/Logo";

interface NavBarProps {
  demoUrl?: string;
}

const NavBar = ({ demoUrl = "/dashboard" }: NavBarProps) => {
  return (
    <nav className="fixed top-0 left-0 right-0 z-50">
      {/* Glass background */}
      <div className="absolute inset-0 glass-ember" />
      
      {/* Ember accent line at bottom */}
      <div className="absolute bottom-0 left-0 right-0 h-px bg-gradient-to-r from-transparent via-ember/30 to-transparent" />
      
      <div className="relative max-w-5xl mx-auto px-4 md:px-6 py-2.5 md:py-3 flex items-center justify-between">
        {/* Logo */}
        <Link to="/" className="flex items-center">
          <Logo size={28} />
        </Link>

        {/* Right side actions */}
        <div className="flex items-center gap-2">
          <Link to={demoUrl}>
            <Button
              className="relative overflow-hidden gradient-ember text-primary-foreground font-semibold px-4 py-1.5 h-auto text-xs md:text-sm transition-all duration-300 hover:scale-105 group"
            >
              <span className="relative z-10">View Demo</span>
              <div className="absolute inset-0 bg-gradient-to-r from-spark to-ember opacity-0 group-hover:opacity-100 transition-opacity duration-300" />
            </Button>
          </Link>
        </div>
      </div>
    </nav>
  );
};

export default NavBar;
