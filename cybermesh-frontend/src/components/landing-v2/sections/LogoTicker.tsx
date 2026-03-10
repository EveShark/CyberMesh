import { motion } from "framer-motion";
import { Cloud, Server, Shield, Network, Lock, Cpu, Monitor, Wifi } from "lucide-react";

const partners = [
  { name: "AWS", icon: Cloud },
  { name: "Kubernetes", icon: Server },
  { name: "CrowdStrike", icon: Shield },
  { name: "Palo Alto", icon: Network },
  { name: "Fortinet", icon: Lock },
  { name: "Azure", icon: Cpu },
  { name: "GCP", icon: Monitor },
  { name: "Cisco", icon: Wifi },
];

const LogoTicker = () => {
  const doubled = [...partners, ...partners];

  return (
    <section className="py-12 px-6 overflow-hidden">
      <motion.div
        initial={{ opacity: 0 }}
        animate={{ opacity: 1 }}
        transition={{ duration: 0.8, delay: 1 }}
      >
        <p className="text-center text-xs font-semibold uppercase tracking-[0.25em] text-muted-foreground mb-8">
          Integrates with your existing stack
        </p>

        <div className="relative max-w-5xl mx-auto">
          <div className="pointer-events-none absolute left-0 top-0 bottom-0 w-20 z-10"
            style={{ background: "linear-gradient(to right, hsl(var(--background)), transparent)" }}
          />
          <div className="pointer-events-none absolute right-0 top-0 bottom-0 w-20 z-10"
            style={{ background: "linear-gradient(to left, hsl(var(--background)), transparent)" }}
          />

          <div className="overflow-hidden">
            <div className="flex items-center gap-12 animate-ticker w-max">
              {doubled.map((p, i) => (
                <div key={`${p.name}-${i}`} className="flex items-center gap-2.5 flex-shrink-0 opacity-40 hover:opacity-70 transition-opacity duration-300">
                  <p.icon className="w-5 h-5 text-primary" />
                  <span className="text-sm font-medium text-primary whitespace-nowrap">{p.name}</span>
                </div>
              ))}
            </div>
          </div>
        </div>
      </motion.div>
    </section>
  );
};

export default LogoTicker;
