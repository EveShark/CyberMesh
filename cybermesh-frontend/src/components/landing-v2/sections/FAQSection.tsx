import { motion, useInView, AnimatePresence } from "framer-motion";
import { useRef, useState } from "react";
import { ChevronRight } from "lucide-react";

const faqs = [
  { q: "What if it blocks real traffic?", a: "We hear this one a lot, and honestly, it is the first thing we worried about too. That is why every pilot starts in passive mode. CyberMesh watches and learns, but does not touch anything until you say so. When it does act, multiple checks have to agree first. You are always in control." },
  { q: "How is this different from Darktrace or CrowdStrike?", a: "Those tools are great at what they do. Darktrace spots anomalies, CrowdStrike guards endpoints. But neither one closes the loop and actually enforces at the network level. We are not here to replace them. We are the missing piece that makes them complete." },
  { q: "Do you store our data?", a: "No. We only look at flow metadata like source, destination, and timing. No deep packet inspection, no payloads, nothing leaves your environment. Your data is yours." },
  { q: "How fast can we get started?", a: "You can have passive mode running in 48 hours. No agents to install on day one. Most teams see their first real detections within the first week of the pilot." },
  { q: "Will it work with what we already use?", a: "Yes. We built CyberMesh to sit alongside your existing stack, whether that is a SIEM, EDR, NDR, or all three. It plugs into Kubernetes and cloud infrastructure natively." },
  { q: "What if CyberMesh goes down?", a: "Your network keeps running. Existing enforcement rules stay active, and no new actions fire until the system recovers. We never become a single point of failure." },
  { q: "Is this built for SaaS teams?", a: "Absolutely. Cloud-native from day one. We understand east-west traffic between your services and enforce policy right at the network fabric, which is exactly where SaaS environments are most exposed." },
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
          <p className="section-label">COMMON QUESTIONS</p>
          <h2 className="text-2xl sm:text-3xl font-bold tracking-tight text-primary mt-4" style={{ fontFamily: "'Playfair Display', Georgia, serif" }}>
            We get asked these a lot.{" "}
            <span className="italic" style={{ color: "hsl(var(--accent))" }}>Here are honest answers.</span>
          </h2>
        </motion.div>

        <div className="grid md:grid-cols-2 gap-x-12 gap-y-0">
          <div>
            {faqs.slice(0, 4).map((faq, i) => (
              <FAQItem key={i} q={faq.q} a={faq.a} index={i} isOpen={openIndex === i} onToggle={() => toggle(i)} inView={inView} />
            ))}
          </div>
          <div>
            {faqs.slice(4).map((faq, i) => (
              <FAQItem key={i + 4} q={faq.q} a={faq.a} index={i + 4} isOpen={openIndex === i + 4} onToggle={() => toggle(i + 4)} inView={inView} />
            ))}
          </div>
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
