import { motion, useInView } from "framer-motion";
import { useRef } from "react";

const WorldviewSection = () => {
  const ref = useRef(null);
  const inView = useInView(ref, { once: true, margin: "-80px" });

  return (
    <section
      ref={ref}
      className="py-24 px-6"
      style={{ background: "hsl(222 50% 7%)" }}
    >
      <div className="mx-auto max-w-6xl">
        <div className="grid lg:grid-cols-[1fr_2fr] gap-16 items-start">
          <motion.div
            initial={{ opacity: 0, y: 20 }}
            animate={inView ? { opacity: 1, y: 0 } : {}}
            transition={{ duration: 0.55 }}
          >
            <p
              className="text-xs font-semibold uppercase tracking-[0.25em]"
              style={{ color: "hsl(42 78% 60%)" }}
            >
              THE FUTURE OF SECURITY
            </p>
            <h2
              className="mt-4 text-3xl sm:text-4xl font-display font-bold tracking-tight leading-tight"
              style={{ color: "hsl(220 20% 94%)" }}
            >
              Security can no longer depend on human reaction time.
            </h2>
          </motion.div>

          <motion.div
            initial={{ opacity: 0, y: 16 }}
            animate={inView ? { opacity: 1, y: 0 } : {}}
            transition={{ duration: 0.55, delay: 0.15 }}
            className="space-y-5 pt-1 lg:pt-12"
          >
            <p
              className="text-base leading-relaxed"
              style={{ color: "hsl(220 15% 62%)" }}
            >
              For decades, security was built on one assumption: a machine detects a threat, a human decides what to do. That assumption is breaking. AI agents, autonomous infrastructure, and machine-speed attacks are compressing response windows from hours to minutes, and minutes to seconds.
            </p>
            <p
              className="text-base leading-relaxed"
              style={{ color: "hsl(220 15% 62%)" }}
            >
              The future of cybersecurity isn't better detection. It's enforcement you can trust to act without you. CyberMesh is built for that future.
            </p>
          </motion.div>
        </div>
      </div>
    </section>
  );
};

export default WorldviewSection;
