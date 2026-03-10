import { motion, useInView } from "framer-motion";
import { useRef } from "react";
import { X, Check } from "lucide-react";

const rows = [
  { old: "Alert fatigue from thousands of uncorrelated signals", now: "Multi-engine consensus reduces noise to actionable events" },
  { old: "Manual triage by overworked SOC analysts", now: "Autonomous validation in milliseconds, no human bottleneck" },
  { old: "4-minute average response time to confirmed threats", now: "Sub-second containment at the infrastructure layer" },
  { old: "Siloed tools that detect but never enforce", now: "Closed-loop system from detection to enforcement" },
];

const ComparisonSection = () => {
  const ref = useRef(null);
  const inView = useInView(ref, { once: true, margin: "-100px" });

  return (
    <section className="py-24 px-6" ref={ref}>
      <div className="mx-auto max-w-5xl">
        <motion.div initial={{ opacity: 0, y: 30 }} animate={inView ? { opacity: 1, y: 0 } : {}} transition={{ duration: 0.6 }} className="text-center">
          <p className="section-label justify-center">THE SHIFT</p>
          <h2 className="section-headline mt-4">From reactive security<br />to autonomous defense.</h2>
        </motion.div>

        <motion.div initial={{ opacity: 0, y: 30 }} animate={inView ? { opacity: 1, y: 0 } : {}} transition={{ duration: 0.6, delay: 0.2 }}
          className="mt-14 rounded-2xl border border-border overflow-hidden" style={{ boxShadow: "0 4px 30px hsl(222 47% 11% / 0.04)" }}>
          <div className="grid grid-cols-2">
            <div className="px-6 sm:px-8 py-5 bg-stat-bg border-b border-r border-border">
              <span className="text-sm font-semibold text-muted-foreground">The Old Way</span>
            </div>
            <div className="px-6 sm:px-8 py-5 bg-primary border-b border-border">
              <span className="text-sm font-semibold text-primary-foreground">The CyberMesh Way</span>
            </div>
          </div>
          {rows.map((row, i) => (
            <motion.div key={i} initial={{ opacity: 0, x: -10 }} animate={inView ? { opacity: 1, x: 0 } : {}} transition={{ duration: 0.4, delay: 0.3 + i * 0.1 }}
              className={`grid grid-cols-2 ${i < rows.length - 1 ? "border-b border-border" : ""}`}>
              <div className="px-6 sm:px-8 py-5 border-r border-border flex items-start gap-3 bg-card">
                <X className="w-4 h-4 text-destructive mt-0.5 flex-shrink-0" />
                <span className="text-sm text-muted-foreground leading-relaxed">{row.old}</span>
              </div>
              <div className="px-6 sm:px-8 py-5 flex items-start gap-3 bg-card">
                <Check className="w-4 h-4 text-accent mt-0.5 flex-shrink-0" />
                <span className="text-sm text-foreground leading-relaxed font-medium">{row.now}</span>
              </div>
            </motion.div>
          ))}
        </motion.div>
      </div>
    </section>
  );
};

export default ComparisonSection;
