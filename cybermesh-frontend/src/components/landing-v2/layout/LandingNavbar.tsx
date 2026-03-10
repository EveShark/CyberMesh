import { useState, useEffect } from "react";
import { Menu, X, ArrowRight } from "lucide-react";
import { motion, AnimatePresence } from "framer-motion";

const DEMO_URL = (import.meta.env.VITE_DEMO_URL as string | undefined)?.trim() || "/dashboard";

const navLinks = [
  { label: "Product", href: "#product" },
  { label: "Why CyberMesh", href: "#problem" },
  { label: "Team", href: "#team" },
];

const Navbar = () => {
  const [scrolled, setScrolled] = useState(false);
  const [mobileOpen, setMobileOpen] = useState(false);

  useEffect(() => {
    const onScroll = () => setScrolled(window.scrollY > 20);
    window.addEventListener("scroll", onScroll);
    return () => window.removeEventListener("scroll", onScroll);
  }, []);

  return (
    <nav
      className={`fixed top-0 left-0 right-0 z-50 transition-all duration-300 ${
        scrolled
          ? "bg-background/90 backdrop-blur-xl border-b border-border/60"
          : "bg-transparent"
      }`}
    >
      <div className="mx-auto max-w-6xl px-6 flex items-center justify-between h-[72px]">
        <a href="#" className="flex items-center gap-2">
          <img
            src="/branding/logo/productos-logo-primary.png"
            alt="CyberMesh logo"
            className="h-8 w-auto"
          />
          <span className="text-xl font-display font-bold text-primary tracking-tight">CyberMesh</span>
        </a>

        <div className="hidden md:flex items-center gap-8">
          {navLinks.map((link) => (
            <a
              key={link.href}
              href={link.href}
              className="text-sm font-medium text-muted-foreground hover:text-primary transition-colors"
            >
              {link.label}
            </a>
          ))}
          <a href={DEMO_URL} className="btn-outline text-xs px-5 py-2.5">
            View Demo
          </a>
          <a href="#contact" className="btn-primary text-xs px-5 py-2.5">
            Request a Pilot <ArrowRight className="w-3.5 h-3.5" />
          </a>
        </div>

        <button
          className="md:hidden text-primary"
          onClick={() => setMobileOpen(!mobileOpen)}
          aria-label="Toggle menu"
        >
          {mobileOpen ? <X size={24} /> : <Menu size={24} />}
        </button>
      </div>

      <AnimatePresence>
        {mobileOpen && (
          <motion.div
            initial={{ opacity: 0, height: 0 }}
            animate={{ opacity: 1, height: "auto" }}
            exit={{ opacity: 0, height: 0 }}
            className="md:hidden bg-background border-b border-border overflow-hidden"
          >
            <div className="px-6 py-4 flex flex-col gap-4">
              {navLinks.map((link) => (
                <a
                  key={link.href}
                  href={link.href}
                  onClick={() => setMobileOpen(false)}
                  className="text-sm font-medium text-muted-foreground hover:text-primary transition-colors"
                >
                  {link.label}
                </a>
              ))}
              <a href={DEMO_URL} onClick={() => setMobileOpen(false)} className="text-sm font-semibold text-primary">
                View Demo
              </a>
              <a href="#contact" onClick={() => setMobileOpen(false)} className="text-sm font-semibold text-accent">
                Request a Pilot
              </a>
            </div>
          </motion.div>
        )}
      </AnimatePresence>
    </nav>
  );
};

export default Navbar;
