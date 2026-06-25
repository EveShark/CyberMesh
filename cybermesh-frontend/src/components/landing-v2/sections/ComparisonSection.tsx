import { motion, useInView } from "framer-motion";
import { useRef } from "react";

const rows = [
  { old: "Alert fatigue from thousands of uncorrelated signals", now: "Independent engines reduce noise to actionable events" },
  { old: "Manual triage by overworked SOC analysts", now: "Autonomous validation in milliseconds, no human bottleneck" },
  { old: "Minutes to respond to a confirmed threat", now: "Sub-second containment at the infrastructure layer" },
  { old: "Siloed tools that detect but never enforce", now: "Closed-loop system from detection to enforcement" },
];

const ComparisonSection = () => {
  const ref = useRef(null);
  const inView = useInView(ref, { once: true, margin: "-100px" });

  return (
    <section className="py-24 px-6 bg-stat-bg border-y border-border" ref={ref}>
      <div className="mx-auto max-w-6xl">
        <motion.div
          initial={{ opacity: 0, y: 20 }}
          animate={inView ? { opacity: 1, y: 0 } : {}}
          transition={{ duration: 0.55 }}
        >
          <h2 className="section-headline">From reactive security<br />to autonomous defense.</h2>
        </motion.div>

        <motion.div
          initial={{ opacity: 0, y: 20 }}
          animate={inView ? { opacity: 1, y: 0 } : {}}
          transition={{ duration: 0.55, delay: 0.15 }}
          className="mt-14 border border-border rounded-xl overflow-hidden bg-background"
        >
          <div className="grid grid-cols-2 border-b border-border">
            <div className="px-6 sm:px-8 py-4 border-r border-border">
              <span className="text-xs font-semibold uppercase tracking-widest text-muted-foreground">Before</span>
            </div>
            <div className="px-6 sm:px-8 py-4 bg-primary">
              <span className="text-xs font-semibold uppercase tracking-widest text-primary-foreground">With CyberMesh</span>
            </div>
          </div>
          {rows.map((row, i) => (
            <div
              key={i}
              className={`grid grid-cols-2 ${i < rows.length - 1 ? "border-b border-border" : ""}`}
            >
              <div className="px-6 sm:px-8 py-5 border-r border-border">
                <span className="text-sm text-muted-foreground/60 leading-relaxed">{row.old}</span>
              </div>
              <div className="px-6 sm:px-8 py-5">
                <span className="text-sm text-foreground leading-relaxed font-medium">{row.now}</span>
              </div>
            </div>
          ))}
        </motion.div>
      </div>
    </section>
  );
};

export default ComparisonSection;
