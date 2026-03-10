import { motion, useInView } from "framer-motion";
import { useRef } from "react";

const team = [
  {
    role: "Founder & CEO",
    badges: ["2x Founder", "AI-Native Products", "Scaled to Market"],
    bio: "Previously built and scaled an AI-native enterprise platform. Knows what it takes to go from zero to production.",
  },
  {
    role: "Lead Technical Advisor",
    badges: ["MIT MSc", "Microsoft", "NASA", "US Naval Academy, Cyber Ops"],
    bio: "Spent his career building and breaking critical infrastructure security systems at the highest levels.",
  },
];

const TeamSection = () => {
  const ref = useRef(null);
  const inView = useInView(ref, { once: true, margin: "-100px" });

  return (
    <section id="team" className="py-24 px-6 bg-stat-bg" ref={ref}>
      <div className="mx-auto max-w-5xl">
        <motion.div
          initial={{ opacity: 0, y: 30 }}
          animate={inView ? { opacity: 1, y: 0 } : {}}
          transition={{ duration: 0.6 }}
          className="text-center"
        >
          <p className="section-label justify-center">THE TEAM</p>
          <h2 className="section-headline mt-4">
            Built by people who have<br />defended real infrastructure.
          </h2>
        </motion.div>

        <div className="mt-14 grid gap-6 sm:grid-cols-2">
          {team.map((t, i) => (
            <motion.div
              key={t.role}
              initial={{ opacity: 0, y: 40, scale: 0.97 }}
              animate={inView ? { opacity: 1, y: 0, scale: 1 } : {}}
              transition={{ duration: 0.6, delay: 0.2 + i * 0.12, ease: [0.21, 0.47, 0.32, 0.98] }}
              className="card-elevated text-center"
            >
              <p className="text-sm font-display font-bold text-accent">{t.role}</p>
              <div className="mt-4 flex flex-wrap justify-center gap-2">
                {t.badges.map((b) => (
                  <span key={b}
                    className="inline-block text-xs font-medium text-primary bg-stat-bg border border-border px-3 py-1.5 rounded-full"
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
