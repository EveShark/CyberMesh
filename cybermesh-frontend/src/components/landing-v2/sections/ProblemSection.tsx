import { motion, useInView } from "framer-motion";
import { useRef } from "react";

const stats = [
  { value: "4 min", desc: "Average human response time to a confirmed threat" },
  { value: "< 1 hr", desc: "Time for an AI agent to achieve full network compromise" },
  { value: "83%", desc: "Of breaches involve the network layer, not endpoints" },
];

const ProblemSection = () => {
  const ref = useRef(null);
  const inView = useInView(ref, { once: true, margin: "-80px" });

  return (
    <section id="problem" className="py-24 px-6 bg-stat-bg border-y border-border" ref={ref}>
      <div className="mx-auto max-w-6xl">
        <div className="grid lg:grid-cols-2 gap-16 items-start">
          <motion.div
            initial={{ opacity: 0, y: 20 }}
            animate={inView ? { opacity: 1, y: 0 } : {}}
            transition={{ duration: 0.55 }}
          >
            <p className="section-label">THE PROBLEM</p>
            <h2 className="section-headline mt-4">
              Detection is solved.<br />Response is broken.
            </h2>
          </motion.div>

          <motion.div
            initial={{ opacity: 0, y: 16 }}
            animate={inView ? { opacity: 1, y: 0 } : {}}
            transition={{ duration: 0.55, delay: 0.12 }}
            className="space-y-4 text-muted-foreground leading-relaxed pt-1 lg:pt-12"
          >
            <p>
              Modern security tools are remarkably good at detecting threats.
              CrowdStrike sees it. Darktrace flags it. Your SIEM alerts. Then
              someone has to act. That window between detection and enforcement
              is where breaches happen.
            </p>
            <p>
              Security teams take minutes to respond. AI-driven attacks compromise
              a network in under an hour. The math does not work.
            </p>
          </motion.div>
        </div>

        <div className="mt-16 grid gap-px bg-border sm:grid-cols-3 rounded-xl overflow-hidden">
          {stats.map((stat, i) => (
            <motion.div
              key={stat.desc}
              initial={{ opacity: 0 }}
              animate={inView ? { opacity: 1 } : {}}
              transition={{ duration: 0.5, delay: 0.2 + i * 0.1 }}
              className="bg-background px-8 py-10"
            >
              <p className="text-4xl font-display font-bold text-primary tabular-nums">
                {stat.value}
              </p>
              <p className="mt-3 text-sm text-muted-foreground leading-relaxed">
                {stat.desc}
              </p>
            </motion.div>
          ))}
        </div>
      </div>
    </section>
  );
};

export default ProblemSection;
