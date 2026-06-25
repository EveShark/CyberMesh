import { motion, useInView } from "framer-motion";
import { useRef } from "react";
import ProductFrame from "@/components/landing-v2/shared/ProductFrame";

const DEMO_URL = (import.meta.env.VITE_DEMO_URL as string | undefined)?.trim() || "/demo";

const ProductShowcaseSection = () => {
  const ref = useRef(null);
  const inView = useInView(ref, { once: true, margin: "-60px" });

  return (
    <section className="pt-20 pb-0 px-6 overflow-hidden" ref={ref}>
      <div className="mx-auto max-w-6xl">
        <motion.div
          initial={{ opacity: 0, y: 20 }}
          animate={inView ? { opacity: 1, y: 0 } : {}}
          transition={{ duration: 0.55 }}
          className="mb-12"
        >
          <p className="section-label">COMMAND CENTER</p>
          <div className="mt-4 flex flex-col sm:flex-row sm:items-end sm:justify-between gap-4">
            <h2 className="text-3xl sm:text-4xl font-display font-bold tracking-tight text-primary max-w-sm leading-tight">
              Every signal. One interface.
            </h2>
            <a
              href={DEMO_URL}
              className="landing-btn-ghost self-start sm:self-auto flex-shrink-0"
            >
              Explore the live demo →
            </a>
          </div>
          <p className="mt-4 text-muted-foreground text-sm leading-relaxed max-w-lg">
            CyberMesh surfaces detection status, consensus health, ledger integrity, and threat telemetry in a unified command view — updated in real time.
          </p>
        </motion.div>

        <motion.div
          initial={{ opacity: 0, y: 40 }}
          animate={inView ? { opacity: 1, y: 0 } : {}}
          transition={{ duration: 0.75, delay: 0.15, ease: [0.21, 0.47, 0.32, 0.98] }}
          className="relative"
        >
          <ProductFrame
            src="/screenshots/dashboard.jpg"
            alt="CyberMesh Command Center — unified dashboard showing Backend Readiness, AI Service Running, Network Health, and API Throughput in real time"
            clipHeight={560}
          />
          <div
            className="absolute bottom-0 left-0 right-0 h-28 pointer-events-none rounded-b-xl"
            style={{
              background: "linear-gradient(to top, hsl(var(--background)), transparent)",
            }}
          />
        </motion.div>
      </div>
    </section>
  );
};

export default ProductShowcaseSection;
