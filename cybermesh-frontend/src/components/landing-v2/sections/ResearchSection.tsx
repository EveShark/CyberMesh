import { motion, useInView } from "framer-motion";
import { useRef } from "react";
import { ExternalLink } from "lucide-react";

const citations = [
  {
    tag: "IEEE SoutheastCon 2026",
    quote: "Decentralized multi-agent systems represent the next frontier in autonomous infrastructure defense.",
    link: "https://arxiv.org/abs/2601.17303",
    source: "Our research on distributed consensus for security-critical systems, submitted to IEEE SoutheastCon 2026",
  },
  {
    tag: "arXiv 2025",
    quote: "AI agents using modern protocols achieved network domain dominance in under one hour, evading all traditional detection measures.",
    link: "https://arxiv.org/abs/2511.15998",
    source: "Our research on AI-agent attack vectors, arXiv 2025",
  },
];

const ResearchSection = () => {
  const ref = useRef(null);
  const inView = useInView(ref, { once: true, margin: "-100px" });

  return (
    <section className="py-24 px-6 bg-stat-bg border-y border-border" ref={ref}>
      <div className="mx-auto max-w-6xl">
        <div className="grid lg:grid-cols-[1fr_2fr] gap-16 items-start">
          <motion.div
            initial={{ opacity: 0, y: 20 }}
            animate={inView ? { opacity: 1, y: 0 } : {}}
            transition={{ duration: 0.55 }}
          >
            <p className="section-label">WHY WE BELIEVE THIS</p>
            <h2 className="section-headline mt-4">Not a hunch.<br />A documented<br />shift.</h2>
          </motion.div>

          <div className="space-y-8 pt-1 lg:pt-12">
            {citations.map((c, i) => (
              <motion.blockquote
                key={i}
                initial={{ opacity: 0, y: 12 }}
                animate={inView ? { opacity: 1, y: 0 } : {}}
                transition={{ duration: 0.45, delay: 0.1 + i * 0.1 }}
                className="border-l-2 border-accent pl-6"
              >
                <p className="text-base text-foreground leading-relaxed font-medium italic">
                  "{c.quote}"
                </p>
                <footer className="mt-4 space-y-1">
                  <p className="text-xs text-muted-foreground leading-relaxed">{c.source}</p>
                  <a
                    href={c.link}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-primary transition-colors"
                  >
                    Read paper <ExternalLink className="w-3 h-3" />
                  </a>
                </footer>
              </motion.blockquote>
            ))}
          </div>
        </div>
      </div>
    </section>
  );
};

export default ResearchSection;
