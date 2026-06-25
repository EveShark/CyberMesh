import { motion } from "framer-motion";
import { ArrowRight } from "lucide-react";
import ProductFrame from "@/components/landing-v2/shared/ProductFrame";

const DEMO_URL = (import.meta.env.VITE_DEMO_URL as string | undefined)?.trim() || "/demo";

const HeroSection = () => {
  return (
    <section className="relative pt-28 sm:pt-36 pb-0 px-6 overflow-hidden">
      <div
        className="pointer-events-none absolute inset-0"
        style={{
          backgroundImage:
            "radial-gradient(circle, hsl(220 15% 72% / 0.3) 1px, transparent 1px)",
          backgroundSize: "28px 28px",
        }}
      />

      <div className="relative mx-auto max-w-6xl">
        <div className="grid lg:grid-cols-[5fr_7fr] gap-12 xl:gap-16 items-center">
          <div>
            <motion.h1
              initial={{ opacity: 0, y: 18 }}
              animate={{ opacity: 1, y: 0 }}
              transition={{ duration: 0.6, delay: 0.05 }}
              className="text-5xl sm:text-6xl lg:text-[3.5rem] xl:text-[4rem] font-display font-bold text-primary tracking-tight leading-[1.06]"
            >
              Trust autonomous defense.{" "}
              <span className="italic" style={{ color: "hsl(var(--accent))" }}>
                Not autonomous mistakes.
              </span>
            </motion.h1>

            <motion.p
              initial={{ opacity: 0, y: 14 }}
              animate={{ opacity: 1, y: 0 }}
              transition={{ duration: 0.55, delay: 0.18 }}
              className="mt-7 text-base text-muted-foreground max-w-md leading-relaxed"
            >
              CyberMesh is the trust layer for autonomous network defense. It detects and contains threats at the infrastructure level, validating every decision before it acts, in under a second.
            </motion.p>

            <motion.div
              initial={{ opacity: 0, y: 10 }}
              animate={{ opacity: 1, y: 0 }}
              transition={{ duration: 0.5, delay: 0.3 }}
              className="mt-9 flex flex-wrap items-center gap-4"
            >
              <a href="#contact" className="landing-btn-primary">
                Request a Pilot <ArrowRight className="w-4 h-4" />
              </a>
              <a href={DEMO_URL} className="landing-btn-secondary">
                View live demo →
              </a>
            </motion.div>

            <motion.p
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              transition={{ duration: 0.5, delay: 0.45 }}
              className="mt-5 text-xs text-muted-foreground/70"
            >
              No commitment · 48-hour deployment · Full report included
            </motion.p>
          </div>

          <motion.div
            initial={{ opacity: 0, y: 32, scale: 0.97 }}
            animate={{ opacity: 1, y: 0, scale: 1 }}
            transition={{ duration: 0.9, delay: 0.3, ease: [0.21, 0.47, 0.32, 0.98] }}
            className="hidden lg:block"
          >
            <ProductFrame
              src="/screenshots/overview.png"
              alt="CyberMesh Command Center — full platform overview showing system health, AI engine status, network topology and blockchain integrity"
              clipHeight={500}
            />
          </motion.div>
        </div>

        <motion.div
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          transition={{ duration: 0.6, delay: 0.6 }}
          className="mt-20 border-t border-border"
        >
          <div className="grid grid-cols-1 sm:grid-cols-3 divide-y sm:divide-y-0 sm:divide-x divide-border">
            {[
              { stat: "< 1s", label: "Containment time", context: "vs. 4-minute industry average" },
              { stat: "Zero-touch", label: "Enforcement", context: "Autonomous from detection to response" },
              { stat: "< 1 hr", label: "What attackers need", context: "For an AI-driven attack to fully compromise a network" },
            ].map((item) => (
              <div key={item.label} className="py-10 sm:px-10 first:pl-0 last:pr-0">
                <p className="text-3xl font-display font-bold text-primary">{item.stat}</p>
                <p className="mt-1 text-sm font-semibold text-foreground">{item.label}</p>
                <p className="mt-1 text-xs text-muted-foreground">{item.context}</p>
              </div>
            ))}
          </div>
        </motion.div>
      </div>
    </section>
  );
};

export default HeroSection;
