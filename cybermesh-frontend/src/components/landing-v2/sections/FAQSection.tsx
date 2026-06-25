import { motion, useInView, AnimatePresence } from "framer-motion";
import { useRef, useState } from "react";
import { ChevronRight } from "lucide-react";

const faqs = [
  { q: "What if it blocks real traffic?", a: "First thing we worried about too. Every pilot starts in passive mode. CyberMesh watches and learns, but doesn't touch anything until you say so. When it does act, multiple checks have to agree first. You're always in control." },
  { q: "How is this different from Darktrace or CrowdStrike?", a: "Those tools are great at what they do. Darktrace spots anomalies, CrowdStrike guards endpoints. Neither closes the loop and enforces at the network level. We're not here to replace them. We're the missing piece that makes them complete." },
  { q: "Do you store our data?", a: "No. We only look at flow metadata: source, destination, timing. No deep packet inspection, no payloads, nothing leaves your environment." },
  { q: "How fast can we get started?", a: "Passive mode running in 48 hours. No agents to install on day one. Most teams see their first real detections within the first week." },
  { q: "Will it work with what we already use?", a: "Yes. CyberMesh sits alongside your existing stack, SIEM, EDR, NDR, or all three. Plugs into Kubernetes and cloud infrastructure natively." },
  { q: "What if CyberMesh goes down?", a: "Your network keeps running. Existing enforcement rules stay active. No new actions fire until the system recovers. We never become a single point of failure." },
  { q: "Is this built for SaaS teams?", a: "Yes. Cloud-native from day one. We understand east-west traffic between your services and enforce policy right at the network fabric, exactly where SaaS environments are most exposed." },
];

const FAQSection = () => {
  const ref = useRef(null);
  const inView = useInView(ref, { once: true, margin: "-60px" });
  const [openIndex, setOpenIndex] = useState<number | null>(null);
  const toggle = (i: number) => setOpenIndex(openIndex === i ? null : i);

  return (
    <section className="py-20 px-6 relative" ref={ref}>
      <div className="mx-auto max-w-5xl relative z-10">
        <motion.div initial={{ opacity: 0, y: 30 }} animate={inView ? { opacity: 1, y: 0 } : {}} transition={{ duration: 0.6 }} className="mb-14">
          <h2 className="text-2xl sm:text-3xl font-bold tracking-tight text-primary" style={{ fontFamily: "'Playfair Display', Georgia, serif" }}>
            We get asked these a lot.{" "}
            <span className="italic" style={{ color: "hsl(var(--accent))" }}>Here are honest answers.</span>
          </h2>
        </motion.div>

        <div className="max-w-3xl">
          {faqs.map((faq, i) => (
            <FAQItem key={i} q={faq.q} a={faq.a} index={i} isOpen={openIndex === i} onToggle={() => toggle(i)} inView={inView} />
          ))}
        </div>
      </div>
    </section>
  );
};

const FAQItem = ({ q, a, index, isOpen, onToggle, inView }: { q: string; a: string; index: number; isOpen: boolean; onToggle: () => void; inView: boolean }) => (
  <motion.div initial={{ opacity: 0, y: 20 }} animate={inView ? { opacity: 1, y: 0 } : {}} transition={{ duration: 0.45, delay: 0.08 + index * 0.06, ease: [0.21, 0.47, 0.32, 0.98] }} className="border-b border-border">
    <button onClick={onToggle} className="w-full flex items-center justify-between gap-3 py-5 text-left group">
      <span className={`text-[13px] font-medium leading-snug transition-colors duration-200 ${isOpen ? "text-accent" : "text-foreground group-hover:text-accent"}`}>{q}</span>
      <ChevronRight className={`w-3.5 h-3.5 flex-shrink-0 transition-all duration-300 ${isOpen ? "rotate-90 text-accent" : "text-muted-foreground group-hover:text-accent"}`} />
    </button>
    <AnimatePresence>
      {isOpen && (
        <motion.div initial={{ height: 0, opacity: 0 }} animate={{ height: "auto", opacity: 1 }} exit={{ height: 0, opacity: 0 }} transition={{ duration: 0.25, ease: "easeInOut" }} className="overflow-hidden">
          <p className="pb-5 text-xs text-muted-foreground leading-relaxed max-w-md">{a}</p>
        </motion.div>
      )}
    </AnimatePresence>
  </motion.div>
);

export default FAQSection;
