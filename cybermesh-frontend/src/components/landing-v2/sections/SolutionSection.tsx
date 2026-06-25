import { motion, useInView } from "framer-motion";
import { useRef } from "react";

const features = [
  {
    num: "01",
    title: "Trust comes from agreement, not authority",
    body: "CyberMesh never lets one model decide alone. Every action requires independent validation first.",
  },
  {
    num: "02",
    title: "Consensus before action",
    body: "Every enforcement action is validated through consensus before it's executed. Independent detection engines have to agree before CyberMesh touches your network.",
  },
  {
    num: "03",
    title: "Works with your infrastructure",
    body: "Deploys natively across Kubernetes, cloud, on-premise, and hybrid environments. No rip-and-replace required.",
  },
  {
    num: "04",
    title: "Gets smarter over time",
    body: "Every enforcement decision feeds back into the detection layer. CyberMesh continuously calibrates to your environment.",
  },
];

const SolutionSection = () => {
  const ref = useRef(null);
  const inView = useInView(ref, { once: true, margin: "-80px" });

  return (
    <section id="product" className="py-24 px-6" ref={ref}>
      <div className="mx-auto max-w-6xl">
        <div className="grid lg:grid-cols-[1fr_2fr] gap-16 items-start">
          <motion.div
            initial={{ opacity: 0, y: 20 }}
            animate={inView ? { opacity: 1, y: 0 } : {}}
            transition={{ duration: 0.55 }}
          >
            <h2 className="section-headline">
              Why autonomous security fails
            </h2>
            <p className="mt-5 text-muted-foreground leading-relaxed text-sm">
              Most security automation relies on a single model, rule engine, or policy system. When it's wrong, the network pays the price.
            </p>
          </motion.div>

          <div className="divide-y divide-border">
            {features.map((f, i) => (
              <motion.div
                key={f.title}
                initial={{ opacity: 0, y: 12 }}
                animate={inView ? { opacity: 1, y: 0 } : {}}
                transition={{ duration: 0.45, delay: 0.1 + i * 0.08 }}
                className="py-8 grid sm:grid-cols-[2rem_1fr] gap-4 sm:gap-8 items-start first:pt-0"
              >
                <span className="text-xs font-mono text-muted-foreground/50 pt-1 hidden sm:block">
                  {f.num}
                </span>
                <div>
                  <h3 className="text-base font-semibold text-primary">{f.title}</h3>
                  <p className="mt-2 text-sm text-muted-foreground leading-relaxed">{f.body}</p>
                </div>
              </motion.div>
            ))}
          </div>
        </div>
      </div>
    </section>
  );
};

export default SolutionSection;
