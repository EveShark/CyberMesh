import { motion, useInView } from "framer-motion";
import { useRef } from "react";
import { ExternalLink } from "lucide-react";

const citations = [
  { tag: "arxiv · 2026", quote: "Decentralized multi-agent systems represent the next frontier in autonomous infrastructure defense.", link: "https://arxiv.org/abs/2601.17303" },
  { tag: "arxiv · 2025", quote: "AI agents using modern protocols achieved network domain dominance in under one hour, evading all traditional detection measures.", link: "https://arxiv.org/abs/2511.15998" },
];

const ResearchSection = () => {
  const ref = useRef(null);
  const inView = useInView(ref, { once: true, margin: "-100px" });

  return (
    <section className="py-24 px-6" ref={ref}>
      <div className="mx-auto max-w-5xl">
        <motion.div initial={{ opacity: 0, y: 30 }} animate={inView ? { opacity: 1, y: 0 } : {}} transition={{ duration: 0.6 }} className="text-center">
          <p className="section-label justify-center">RESEARCH BACKED</p>
          <h2 className="section-headline mt-4">The threat is real.<br />The solution is proven.</h2>
          <p className="mt-4 text-muted-foreground max-w-2xl mx-auto leading-relaxed">
            CyberMesh is built on principles validated by peer-reviewed academic research in autonomous network security and AI-powered attack methodologies.
          </p>
        </motion.div>

        <div className="mt-14 grid gap-6 sm:grid-cols-2">
          {citations.map((c, i) => (
            <motion.div key={i} initial={{ opacity: 0, y: 40, scale: 0.97 }} animate={inView ? { opacity: 1, y: 0, scale: 1 } : {}}
              transition={{ duration: 0.6, delay: 0.2 + i * 0.12, ease: [0.21, 0.47, 0.32, 0.98] }} className="card-elevated">
              <span className="inline-block text-xs font-semibold text-accent bg-stat-bg px-3 py-1.5 rounded-full border border-border">{c.tag}</span>
              <p className="mt-5 text-sm text-muted-foreground leading-relaxed italic">"{c.quote}"</p>
              <a href={c.link} target="_blank" rel="noopener noreferrer"
                className="mt-5 inline-flex items-center gap-1.5 text-sm font-medium text-primary hover:text-accent transition-colors">
                Read the research <ExternalLink className="w-3.5 h-3.5" />
              </a>
            </motion.div>
          ))}
        </div>
      </div>
    </section>
  );
};

export default ResearchSection;
