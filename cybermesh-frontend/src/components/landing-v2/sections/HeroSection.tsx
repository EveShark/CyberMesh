import { motion } from "framer-motion";
import { Shield, CheckCircle, Lock, ArrowRight } from "lucide-react";
import MeshBackground from "@/components/landing-v2/shared/MeshBackground";

const DEMO_URL = (import.meta.env.VITE_DEMO_URL as string | undefined)?.trim() || "/dashboard";

const steps = [
  { icon: Shield, label: "Threat Detected" },
  { icon: CheckCircle, label: "Consensus Validated" },
  { icon: Lock, label: "Automatically Contained" },
];

const HeroSection = () => {
  return (
    <section className="relative pt-36 pb-12 sm:pt-48 sm:pb-16 px-6 overflow-hidden">
      <MeshBackground />
      <div className="pointer-events-none absolute top-0 left-1/2 -translate-x-1/2 w-[800px] h-[600px] opacity-[0.07]"
        style={{ background: "radial-gradient(ellipse, hsl(42 78% 60%), transparent 70%)" }}
      />

      <div className="relative mx-auto max-w-4xl text-center">
        <motion.h1
          initial={{ opacity: 0, y: 30 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.8 }}
          className="section-headline text-4xl sm:text-5xl lg:text-[3.75rem] leading-[1.12]"
        >
          Your Network Responds<br className="hidden sm:block" /> Before Your Team Can React.
        </motion.h1>

        <motion.p
          initial={{ opacity: 0, y: 20 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.7, delay: 0.15 }}
          className="mt-7 text-lg text-muted-foreground max-w-2xl mx-auto leading-relaxed"
        >
          CyberMesh autonomously detects and contains network threats at the
          infrastructure level, with built-in safety guarantees that ensure
          it never acts without certainty.
        </motion.p>

        <motion.div
          initial={{ opacity: 0, y: 20 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.7, delay: 0.3 }}
          className="mt-10 flex flex-col sm:flex-row items-center justify-center gap-4"
        >
          <a href="#contact" className="btn-gold">
            Request a Pilot <ArrowRight className="w-4 h-4" />
          </a>
          <a href={DEMO_URL} className="btn-outline">
            View Demo
          </a>
        </motion.div>

        <motion.p
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          transition={{ duration: 0.7, delay: 0.5 }}
          className="mt-6 text-xs text-muted-foreground tracking-wide"
        >
          Trusted by security teams who cannot afford a 4-minute response window.
        </motion.p>

        <motion.div
          initial={{ opacity: 0, y: 30 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.8, delay: 0.6 }}
          className="mt-20 flex items-center justify-center gap-2 sm:gap-6"
        >
          {steps.map((step, i) => (
            <div key={step.label} className="flex items-center gap-2 sm:gap-6">
              <motion.div
                initial={{ scale: 0 }}
                animate={{ scale: 1 }}
                transition={{ delay: 0.8 + i * 0.2, type: "spring", stiffness: 200 }}
                className="flex flex-col items-center gap-3"
              >
                <div className="w-14 h-14 sm:w-16 sm:h-16 rounded-2xl bg-card border border-border flex items-center justify-center animate-pulse-glow"
                  style={{ boxShadow: "0 2px 12px hsl(222 47% 11% / 0.06)" }}
                >
                  <step.icon className="w-6 h-6 sm:w-7 sm:h-7 text-accent" />
                </div>
                <span className="text-[11px] sm:text-sm font-medium text-primary whitespace-nowrap">
                  {step.label}
                </span>
              </motion.div>

              {i < steps.length - 1 && (
                <svg width="50" height="2" className="hidden sm:block flex-shrink-0">
                  <line x1="0" y1="1" x2="50" y2="1"
                    stroke="hsl(42 78% 60%)" strokeWidth="2" strokeDasharray="6 6"
                    className="animate-flow-dots"
                  />
                </svg>
              )}
            </div>
          ))}
        </motion.div>
      </div>
    </section>
  );
};

export default HeroSection;
