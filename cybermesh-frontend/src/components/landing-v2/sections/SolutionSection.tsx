import { motion, useInView } from "framer-motion";
import { useRef } from "react";
import { Brain, ShieldCheck, Plug, RefreshCw } from "lucide-react";

const features = [
  { icon: Brain, title: "Multi-Layer Detection", body: "Every threat is analyzed across multiple detection engines simultaneously. No single point of failure, no single model making unilateral decisions." },
  { icon: ShieldCheck, title: "Consensus Before Action", body: "Before CyberMesh touches your network, a distributed validation layer reaches agreement. Autonomous does not mean reckless." },
  { icon: Plug, title: "Works With Your Infrastructure", body: "Deploys natively across Kubernetes, cloud, on-premise, and hybrid environments. No rip-and-replace." },
  { icon: RefreshCw, title: "Gets Smarter Over Time", body: "Every enforcement decision feeds back into the detection layer. CyberMesh continuously calibrates to your environment." },
];

const SolutionSection = () => {
  const ref = useRef(null);
  const inView = useInView(ref, { once: true, margin: "-80px" });

  return (
    <section id="product" className="py-24 px-6" ref={ref}>
      <div className="mx-auto max-w-5xl">
        <motion.div initial={{ opacity: 0, y: 30 }} animate={inView ? { opacity: 1, y: 0 } : {}} transition={{ duration: 0.6 }} className="text-center">
          <p className="section-label justify-center">THE SOLUTION</p>
          <h2 className="section-headline mt-4">Autonomous enforcement.<br />With the safety to trust it.</h2>
          <p className="mt-4 text-lg text-muted-foreground max-w-2xl mx-auto">CyberMesh closes the loop from detection to enforcement in milliseconds. Not minutes.</p>
        </motion.div>

        <div className="mt-14 grid gap-6 sm:grid-cols-2">
          {features.map((f, i) => (
            <motion.div key={f.title} initial={{ opacity: 0, y: 40, scale: 0.97 }} animate={inView ? { opacity: 1, y: 0, scale: 1 } : {}}
              transition={{ duration: 0.6, delay: 0.15 * i, ease: [0.21, 0.47, 0.32, 0.98] }} className="card-elevated group">
              <div className="w-12 h-12 rounded-xl bg-stat-bg border border-border flex items-center justify-center mb-5 group-hover:bg-primary group-hover:border-primary transition-all duration-300">
                <f.icon className="w-5 h-5 text-accent group-hover:text-primary-foreground transition-colors duration-300" />
              </div>
              <h3 className="text-lg font-display font-bold text-primary">{f.title}</h3>
              <p className="mt-3 text-sm text-muted-foreground leading-relaxed">{f.body}</p>
            </motion.div>
          ))}
        </div>
      </div>
    </section>
  );
};

export default SolutionSection;
