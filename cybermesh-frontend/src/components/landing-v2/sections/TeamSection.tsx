import { motion, useInView } from "framer-motion";
import { useRef } from "react";

const team = [
  {
    role: "Founder and CEO",
    credentials: ["3× Founder", "Offensive Security Background"],
    bio: "Built and shipped AI-native products before this. Knows what it takes to go from zero to production in high-stakes environments.",
  },
  {
    role: "Lead Technical Advisor",
    credentials: ["MIT MSc", "Microsoft", "NASA", "US Naval Academy", "Cyber Ops"],
    bio: "Spent his career building and breaking critical infrastructure security systems at the highest levels of government and industry.",
  },
];

const TeamSection = () => {
  const ref = useRef(null);
  const inView = useInView(ref, { once: true, margin: "-100px" });

  return (
    <section id="team" className="py-24 px-6" ref={ref}>
      <div className="mx-auto max-w-6xl">
        <motion.div
          initial={{ opacity: 0, y: 20 }}
          animate={inView ? { opacity: 1, y: 0 } : {}}
          transition={{ duration: 0.55 }}
        >
          <h2 className="section-headline">
            Built by people who have<br />defended real infrastructure.
          </h2>
        </motion.div>

        <div className="mt-14 grid gap-px bg-border sm:grid-cols-2 rounded-xl overflow-hidden">
          {team.map((t, i) => (
            <motion.div
              key={t.role}
              initial={{ opacity: 0 }}
              animate={inView ? { opacity: 1 } : {}}
              transition={{ duration: 0.5, delay: 0.15 + i * 0.1 }}
              className="bg-background p-8 sm:p-10"
            >
              <p className="text-xs font-semibold uppercase tracking-[0.18em] text-accent">{t.role}</p>
              <div className="mt-4 flex flex-wrap gap-2">
                {t.credentials.map((b) => (
                  <span
                    key={b}
                    className="inline-block text-xs font-medium text-muted-foreground border border-border px-2.5 py-1 rounded-md"
                  >
                    {b}
                  </span>
                ))}
              </div>
              <p className="mt-5 text-sm text-muted-foreground leading-relaxed">{t.bio}</p>
            </motion.div>
          ))}
        </div>
      </div>
    </section>
  );
};

export default TeamSection;
