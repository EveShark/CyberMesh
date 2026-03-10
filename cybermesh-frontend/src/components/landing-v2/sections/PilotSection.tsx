import { motion, useInView } from "framer-motion";
import { useRef } from "react";
import { ArrowRight } from "lucide-react";

const steps = [
  { period: "Week 1-2", title: "Deploy & Observe", desc: "CyberMesh runs in passive mode on your network. It detects and logs every threat it would have acted on, without touching anything. You see everything." },
  { period: "Week 3-4", title: "Tune & Validate", desc: "We review detections together and tune thresholds to your environment. You decide what good looks like." },
  { period: "Week 5-6", title: "Go Live", desc: "Flip the switch on one network segment. CyberMesh enforces in real time. You stay in control." },
];

const PilotSection = () => {
  const ref = useRef(null);
  const inView = useInView(ref, { once: true, margin: "-100px" });

  return (
    <section className="py-24 px-6 bg-stat-bg" ref={ref}>
      <div className="mx-auto max-w-5xl">
        <motion.div initial={{ opacity: 0, y: 30 }} animate={inView ? { opacity: 1, y: 0 } : {}} transition={{ duration: 0.6 }} className="text-center">
          <p className="section-label justify-center">GET STARTED</p>
          <h2 className="section-headline mt-4">From zero to protected<br />in two weeks.</h2>
        </motion.div>

        <div className="mt-16 relative">
          <div className="hidden sm:block absolute top-7 left-[16.67%] right-[16.67%] h-px bg-border" />
          <div className="grid gap-10 sm:grid-cols-3">
            {steps.map((step, i) => (
              <motion.div key={step.title} initial={{ opacity: 0, y: 40, scale: 0.97 }} animate={inView ? { opacity: 1, y: 0, scale: 1 } : {}}
                transition={{ duration: 0.6, delay: 0.2 + i * 0.15, ease: [0.21, 0.47, 0.32, 0.98] }} className="relative text-center">
                <div className="w-14 h-14 rounded-full bg-card border-2 border-accent flex items-center justify-center text-accent text-sm font-bold mx-auto mb-6 relative z-10"
                  style={{ boxShadow: "0 4px 15px hsl(42 78% 60% / 0.15)" }}>{i + 1}</div>
                <p className="text-xs font-semibold uppercase tracking-[0.2em] text-accent">{step.period}</p>
                <h3 className="mt-1 text-lg font-display font-bold text-primary">{step.title}</h3>
                <p className="mt-3 text-sm text-muted-foreground leading-relaxed">{step.desc}</p>
              </motion.div>
            ))}
          </div>
        </div>

        <motion.div initial={{ opacity: 0 }} animate={inView ? { opacity: 1 } : {}} transition={{ duration: 0.6, delay: 0.8 }} className="mt-14 text-center">
          <p className="text-sm text-muted-foreground max-w-2xl mx-auto mb-8">
            At the end of the pilot, you receive a full report showing exactly what was detected, what was blocked, and how your response time changed.
          </p>
          <a href="#contact" className="btn-gold">Request Your Pilot <ArrowRight className="w-4 h-4" /></a>
        </motion.div>
      </div>
    </section>
  );
};

export default PilotSection;
