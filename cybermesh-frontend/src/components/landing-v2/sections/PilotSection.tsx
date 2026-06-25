import { motion, useInView } from "framer-motion";
import { useRef } from "react";
import { ArrowRight } from "lucide-react";

const steps = [
  { period: "Week 1–2", title: "Deploy and observe", desc: "CyberMesh runs in passive mode. It logs every threat it would have acted on, without touching anything. You see everything." },
  { period: "Week 3–4", title: "Tune and validate", desc: "We review detections together and tune thresholds to your environment. You decide what good looks like." },
  { period: "Week 5–6", title: "Go live", desc: "Flip the switch on one network segment. CyberMesh enforces in real time. You stay in control." },
];

const PilotSection = () => {
  const ref = useRef(null);
  const inView = useInView(ref, { once: true, margin: "-100px" });

  return (
    <section className="py-24 px-6" ref={ref}>
      <div className="mx-auto max-w-6xl">
        <div className="grid lg:grid-cols-[1fr_2fr] gap-16 items-start">
          <motion.div
            initial={{ opacity: 0, y: 20 }}
            animate={inView ? { opacity: 1, y: 0 } : {}}
            transition={{ duration: 0.55 }}
          >
            <h2 className="section-headline">From zero to protected<br />in six weeks.</h2>
            <p className="mt-5 text-sm text-muted-foreground leading-relaxed">
              At the end of the pilot, you get a full report: what was detected, what was blocked, and how your response time changed.
            </p>
            <a href="#contact" className="landing-btn-primary mt-8 inline-flex">
              Request a Pilot <ArrowRight className="w-4 h-4" />
            </a>
          </motion.div>

          <div className="divide-y divide-border">
            {steps.map((step, i) => (
              <motion.div
                key={step.title}
                initial={{ opacity: 0, y: 12 }}
                animate={inView ? { opacity: 1, y: 0 } : {}}
                transition={{ duration: 0.45, delay: 0.1 + i * 0.1 }}
                className="py-8 grid grid-cols-[3rem_1fr] gap-6 items-start first:pt-0"
              >
                <span className="text-2xl font-display font-bold text-border tabular-nums">
                  {String(i + 1).padStart(2, "0")}
                </span>
                <div>
                  <p className="text-[11px] font-semibold uppercase tracking-[0.18em] text-muted-foreground mb-1">{step.period}</p>
                  <h3 className="text-base font-semibold text-primary">{step.title}</h3>
                  <p className="mt-2 text-sm text-muted-foreground leading-relaxed">{step.desc}</p>
                </div>
              </motion.div>
            ))}
          </div>
        </div>
      </div>
    </section>
  );
};

export default PilotSection;
