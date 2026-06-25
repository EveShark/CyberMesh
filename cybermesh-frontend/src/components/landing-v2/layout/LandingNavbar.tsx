import { useState, useEffect } from "react";
import { Menu, X, ArrowRight } from "lucide-react";
import { motion, AnimatePresence } from "framer-motion";

const navLinks = [
  { label: "Product", href: "#product" },
  { label: "Why CyberMesh", href: "#problem" },
  { label: "Team", href: "#team" },
];

const Navbar = () => {
  const [scrolled, setScrolled] = useState(false);
  const [mobileOpen, setMobileOpen] = useState(false);
  const [pastHero, setPastHero] = useState(false);

  useEffect(() => {
    const onScroll = () => {
      setScrolled(window.scrollY > 20);
      setPastHero(window.scrollY > 480);
    };
    window.addEventListener("scroll", onScroll, { passive: true });
    return () => window.removeEventListener("scroll", onScroll);
  }, []);

  useEffect(() => {
    if (mobileOpen) {
      document.body.style.overflow = "hidden";
    } else {
      document.body.style.overflow = "";
    }
    return () => { document.body.style.overflow = ""; };
  }, [mobileOpen]);

  return (
    <>
      <nav
        className={`fixed top-0 left-0 right-0 z-50 transition-all duration-300 ${
          scrolled
            ? "bg-background/90 backdrop-blur-xl border-b border-border/60"
            : "bg-transparent"
        }`}
      >
        <div className="mx-auto max-w-6xl px-6 flex items-center justify-between h-[68px]">
          <a href="#" className="flex items-center gap-2 shrink-0">
            <img
              src="/branding/logo/productos-logo-primary.png"
              alt="CyberMesh logo"
              className="h-7 w-auto"
            />
            <span className="text-lg font-display font-bold text-primary tracking-tight">
              CyberMesh
            </span>
          </a>

          {/* Desktop nav */}
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
            <a href="#contact" className="landing-btn-primary text-xs px-5 py-2.5">
              Request a Pilot <ArrowRight className="w-3.5 h-3.5" />
            </a>
          </div>

          {/* Mobile hamburger */}
          <button
            className="md:hidden relative z-[60] flex items-center justify-center w-10 h-10 rounded-lg text-primary hover:bg-muted/50 transition-colors"
            onClick={() => setMobileOpen(!mobileOpen)}
            aria-label={mobileOpen ? "Close menu" : "Open menu"}
          >
            <AnimatePresence mode="wait" initial={false}>
              {mobileOpen ? (
                <motion.span
                  key="close"
                  initial={{ rotate: -90, opacity: 0 }}
                  animate={{ rotate: 0, opacity: 1 }}
                  exit={{ rotate: 90, opacity: 0 }}
                  transition={{ duration: 0.18 }}
                >
                  <X size={22} />
                </motion.span>
              ) : (
                <motion.span
                  key="open"
                  initial={{ rotate: 90, opacity: 0 }}
                  animate={{ rotate: 0, opacity: 1 }}
                  exit={{ rotate: -90, opacity: 0 }}
                  transition={{ duration: 0.18 }}
                >
                  <Menu size={22} />
                </motion.span>
              )}
            </AnimatePresence>
          </button>
        </div>
      </nav>

      {/* Mobile drawer + backdrop */}
      <AnimatePresence>
        {mobileOpen && (
          <>
            {/* Backdrop */}
            <motion.div
              key="backdrop"
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              exit={{ opacity: 0 }}
              transition={{ duration: 0.22 }}
              className="fixed inset-0 z-40 bg-black/30 backdrop-blur-sm md:hidden"
              onClick={() => setMobileOpen(false)}
            />

            {/* Drawer panel */}
            <motion.div
              key="drawer"
              initial={{ x: "100%" }}
              animate={{ x: 0 }}
              exit={{ x: "100%" }}
              transition={{ type: "spring", stiffness: 320, damping: 34 }}
              className="fixed top-0 right-0 bottom-0 z-50 md:hidden flex flex-col w-72 bg-background border-l border-border shadow-2xl"
            >
              {/* Drawer header */}
              <div className="flex items-center justify-between px-6 h-[68px] border-b border-border/60 shrink-0">
                <span className="text-sm font-semibold text-primary">Menu</span>
                <button
                  onClick={() => setMobileOpen(false)}
                  className="flex items-center justify-center w-9 h-9 rounded-lg hover:bg-muted/60 transition-colors text-muted-foreground"
                  aria-label="Close menu"
                >
                  <X size={18} />
                </button>
              </div>

              {/* Nav links */}
              <div className="flex-1 overflow-y-auto px-4 py-6 flex flex-col gap-1">
                {navLinks.map((link, i) => (
                  <motion.a
                    key={link.href}
                    href={link.href}
                    onClick={() => setMobileOpen(false)}
                    initial={{ opacity: 0, x: 20 }}
                    animate={{ opacity: 1, x: 0 }}
                    transition={{ delay: i * 0.06 + 0.08 }}
                    className="flex items-center px-3 py-3.5 rounded-lg text-base font-medium text-foreground hover:bg-muted/60 transition-colors"
                  >
                    {link.label}
                  </motion.a>
                ))}
              </div>

              {/* CTA at bottom of drawer */}
              <div className="px-4 pb-8 pt-4 border-t border-border/60 shrink-0">
                <a
                  href="#contact"
                  onClick={() => setMobileOpen(false)}
                  className="landing-btn-primary w-full justify-center text-sm"
                >
                  Request a Pilot <ArrowRight className="w-4 h-4" />
                </a>
                <p className="mt-3 text-center text-[11px] text-muted-foreground">
                  No commitment · 48-hour deployment
                </p>
              </div>
            </motion.div>
          </>
        )}
      </AnimatePresence>

      {/* Sticky bottom CTA — mobile only, visible after hero scrolls past */}
      <AnimatePresence>
        {pastHero && !mobileOpen && (
          <motion.div
            key="bottom-cta"
            initial={{ y: 80, opacity: 0 }}
            animate={{ y: 0, opacity: 1 }}
            exit={{ y: 80, opacity: 0 }}
            transition={{ type: "spring", stiffness: 340, damping: 34 }}
            className="fixed bottom-0 left-0 right-0 z-40 md:hidden px-4 pb-5 pt-3"
            style={{
              background:
                "linear-gradient(to top, hsl(220 20% 98%) 70%, hsl(220 20% 98% / 0))",
            }}
          >
            <a
              href="#contact"
              className="landing-btn-primary w-full justify-center text-sm py-3.5 shadow-lg"
              style={{
                boxShadow:
                  "0 8px 28px -6px hsl(222 47% 11% / 0.28), 0 2px 8px -2px hsl(222 47% 11% / 0.14)",
              }}
            >
              Request a Pilot <ArrowRight className="w-4 h-4" />
            </a>
          </motion.div>
        )}
      </AnimatePresence>
    </>
  );
};

export default Navbar;
