import { motion, useInView, animate } from "framer-motion";
import { useRef, useEffect, useState } from "react";

const stats = [
  { target: 4, suffix: " min", prefix: "", desc: "Average human response time to a confirmed threat" },
  { target: 1, suffix: " hr", prefix: "< ", desc: "Time for an AI agent to achieve full network compromise (MIT, 2025)" },
  { target: 83, suffix: "%", prefix: "", desc: "Of breaches involve the network layer, not just endpoints" },
];

const CountUp = ({ target, prefix, suffix, inView }: { target: number; prefix: string; suffix: string; inView: boolean }) => {
  const [display, setDisplay] = useState(0);
  useEffect(() => {
    if (!inView) return;
    const controls = animate(0, target, {
      duration: 1.5, ease: "easeOut",
      onUpdate: (v) => setDisplay(Math.round(v)),
    });
    return () => controls.stop();
  }, [inView, target]);
  return <>{prefix}{display}{suffix}</>;
};

const ProblemSection = () => {
  const ref = useRef(null);
  const inView = useInView(ref, { once: true, margin: "-100px" });

  return (
    <section id="problem" className="py-24 px-6" ref={ref}>
      <div className="mx-auto max-w-5xl">
        <motion.div
          initial={{ opacity: 0, y: 30 }}
          animate={inView ? { opacity: 1, y: 0 } : {}}
          transition={{ duration: 0.6 }}
          className="text-center"
        >
          <p className="section-label justify-center">THE PROBLEM</p>
          <h2 className="section-headline mt-4">
            Detection is solved.<br />Response is broken.
          </h2>
        </motion.div>

        <motion.div
          initial={{ opacity: 0, y: 20 }}
          animate={inView ? { opacity: 1, y: 0 } : {}}
          transition={{ duration: 0.6, delay: 0.2 }}
          className="mt-8 max-w-3xl mx-auto space-y-4 text-muted-foreground leading-relaxed text-center"
        >
          <p>
            Modern security tools are remarkably good at detecting threats.
            CrowdStrike sees it. Darktrace flags it. Your SIEM alerts. And then
            someone has to act. That window between detection and enforcement
            is where breaches happen.
          </p>
          <p>
            The average security team takes 4 minutes to manually respond to a
            confirmed threat. An AI-powered attack can achieve full network
            compromise in under an hour. The math does not work.
          </p>
        </motion.div>

        <div className="mt-14 grid gap-6 sm:grid-cols-3">
          {stats.map((stat, i) => (
            <motion.div
              key={stat.desc}
              initial={{ opacity: 0, y: 40, scale: 0.97 }}
              animate={inView ? { opacity: 1, y: 0, scale: 1 } : {}}
              transition={{ duration: 0.6, delay: 0.3 + i * 0.12, ease: [0.21, 0.47, 0.32, 0.98] }}
              className="card-elevated text-center"
            >
              <p className="text-3xl font-display font-bold text-accent">
                <CountUp target={stat.target} prefix={stat.prefix} suffix={stat.suffix} inView={inView} />
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
