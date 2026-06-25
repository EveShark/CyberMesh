import { useState, useRef, useCallback } from "react";
import { motion, useInView, AnimatePresence } from "framer-motion";
import { ArrowRight, Loader2, CheckCircle2 } from "lucide-react";
import { mailApi } from "@/lib/mail-api";

const PILOT_INCLUDES = [
  "48-hour deployment",
  "6-week protected period",
  "Full threat analysis report",
  "No lock-in, cancel anytime",
];

const TEAM_SIZES = [
  "1–50 employees",
  "50–200 employees",
  "200–1,000 employees",
  "1,000–5,000 employees",
  "5,000+ employees",
];

interface FormState {
  name: string;
  email: string;
  company: string;
  teamSize: string;
  message: string;
}

const inputBase =
  "w-full rounded-lg px-4 py-3 text-sm text-white placeholder:text-white/30 outline-none transition-all duration-200 bg-white/[0.06] border border-white/[0.12] focus:border-white/30 focus:bg-white/[0.09]";

const ContactSection = () => {
  const ref = useRef(null);
  const inView = useInView(ref, { once: true, margin: "-80px" });

  const [form, setForm] = useState<FormState>({
    name: "",
    email: "",
    company: "",
    teamSize: "",
    message: "",
  });
  const [submitting, setSubmitting] = useState(false);
  const [submitted, setSubmitted] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const set = useCallback(
    (field: keyof FormState) =>
      (e: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement>) => {
        setForm((prev) => ({ ...prev, [field]: e.target.value }));
        if (error) setError(null);
      },
    [error]
  );

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (submitting || submitted) return;

    if (!form.name.trim() || !form.email.trim()) {
      setError("Name and email are required.");
      return;
    }

    setSubmitting(true);
    setError(null);

    try {
      const messageBody = [
        form.message.trim() && `Message: ${form.message.trim()}`,
        form.teamSize && `Team size: ${form.teamSize}`,
      ]
        .filter(Boolean)
        .join("\n");

      await mailApi.sendContactForm({
        name: form.name.trim(),
        email: form.email.trim(),
        company: form.company.trim() || undefined,
        message: messageBody || "Pilot request submitted via landing page.",
      });

      setSubmitted(true);
    } catch (err) {
      setError(
        err instanceof Error ? err.message : "Something went wrong. Please try again."
      );
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <section
      id="contact"
      ref={ref}
      className="py-24 px-4 sm:px-6"
      style={{ background: "hsl(222 47% 11%)" }}
    >
      <div className="mx-auto max-w-6xl">
        <div className="grid lg:grid-cols-[5fr_7fr] gap-16 xl:gap-24 items-start">

          {/* Left column */}
          <motion.div
            initial={{ opacity: 0, y: 20 }}
            animate={inView ? { opacity: 1, y: 0 } : {}}
            transition={{ duration: 0.55 }}
          >
            <p
              className="text-xs font-semibold uppercase tracking-[0.25em] mb-6"
              style={{ color: "hsl(42 78% 60%)" }}
            >
              REQUEST A PILOT
            </p>

            <h2
              className="text-3xl sm:text-4xl font-display font-bold tracking-tight leading-tight"
              style={{ color: "hsl(220 20% 96%)" }}
            >
              Let's talk about<br />your network.
            </h2>

            <p className="mt-5 text-sm leading-relaxed" style={{ color: "hsl(220 15% 56%)" }}>
              Tell us about your environment. We'll get back to you within 24 hours to schedule your pilot.
            </p>

            <ul className="mt-10 space-y-3">
              {PILOT_INCLUDES.map((text, i) => (
                <motion.li
                  key={text}
                  initial={{ opacity: 0, x: -10 }}
                  animate={inView ? { opacity: 1, x: 0 } : {}}
                  transition={{ duration: 0.4, delay: 0.15 + i * 0.07 }}
                  className="flex items-center gap-3"
                >
                  <span
                    className="w-1 h-1 rounded-full flex-shrink-0"
                    style={{ background: "hsl(42 78% 60%)" }}
                  />
                  <span className="text-sm" style={{ color: "hsl(220 15% 66%)" }}>
                    {text}
                  </span>
                </motion.li>
              ))}
            </ul>

          </motion.div>

          {/* Right column — form */}
          <motion.div
            initial={{ opacity: 0, y: 24 }}
            animate={inView ? { opacity: 1, y: 0 } : {}}
            transition={{ duration: 0.6, delay: 0.12 }}
            className="relative"
          >
            <div
              className="rounded-2xl p-8 sm:p-10"
              style={{
                background: "hsl(222 44% 14%)",
                border: "1px solid hsl(220 30% 22%)",
                boxShadow: "0 32px 80px -20px hsl(222 47% 6% / 0.7)",
              }}
            >
              <AnimatePresence mode="wait">
                {submitted ? (
                  <motion.div
                    key="success"
                    initial={{ opacity: 0, scale: 0.96 }}
                    animate={{ opacity: 1, scale: 1 }}
                    transition={{ duration: 0.4 }}
                    className="py-12 flex flex-col items-center text-center gap-5"
                  >
                    <div
                      className="w-16 h-16 rounded-full flex items-center justify-center"
                      style={{
                        background: "hsl(142 71% 45% / 0.12)",
                        border: "1px solid hsl(142 71% 45% / 0.3)",
                      }}
                    >
                      <CheckCircle2 className="w-8 h-8" style={{ color: "hsl(142 71% 55%)" }} />
                    </div>
                    <div>
                      <h3 className="text-lg font-semibold" style={{ color: "hsl(220 20% 94%)" }}>
                        Request received
                      </h3>
                      <p className="mt-2 text-sm" style={{ color: "hsl(220 15% 52%)" }}>
                        We'll reach out within 24 hours to schedule your pilot kickoff.
                      </p>
                    </div>
                  </motion.div>
                ) : (
                  <motion.form
                    key="form"
                    onSubmit={handleSubmit}
                    className="space-y-5"
                    initial={{ opacity: 1 }}
                    exit={{ opacity: 0 }}
                  >
                    <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                      <div className="flex flex-col gap-1.5">
                        <label className="text-xs font-medium" style={{ color: "hsl(220 15% 52%)" }}>
                          Full name <span style={{ color: "hsl(42 78% 60%)" }}>*</span>
                        </label>
                        <input
                          type="text"
                          required
                          placeholder="Alex Johnson"
                          value={form.name}
                          onChange={set("name")}
                          className={inputBase}
                        />
                      </div>
                      <div className="flex flex-col gap-1.5">
                        <label className="text-xs font-medium" style={{ color: "hsl(220 15% 52%)" }}>
                          Work email <span style={{ color: "hsl(42 78% 60%)" }}>*</span>
                        </label>
                        <input
                          type="email"
                          required
                          placeholder="alex@company.com"
                          value={form.email}
                          onChange={set("email")}
                          className={inputBase}
                        />
                      </div>
                    </div>

                    <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                      <div className="flex flex-col gap-1.5">
                        <label className="text-xs font-medium" style={{ color: "hsl(220 15% 52%)" }}>
                          Company
                        </label>
                        <input
                          type="text"
                          placeholder="Acme Corp"
                          value={form.company}
                          onChange={set("company")}
                          className={inputBase}
                        />
                      </div>
                      <div className="flex flex-col gap-1.5">
                        <label className="text-xs font-medium" style={{ color: "hsl(220 15% 52%)" }}>
                          Team size
                        </label>
                        <select
                          value={form.teamSize}
                          onChange={set("teamSize")}
                          className={inputBase}
                          style={{ cursor: "pointer" }}
                        >
                          <option value="" style={{ background: "hsl(222 44% 14%)" }}>Select...</option>
                          {TEAM_SIZES.map((s) => (
                            <option key={s} value={s} style={{ background: "hsl(222 44% 14%)" }}>
                              {s}
                            </option>
                          ))}
                        </select>
                      </div>
                    </div>

                    <div className="flex flex-col gap-1.5">
                      <label className="text-xs font-medium" style={{ color: "hsl(220 15% 52%)" }}>
                        What are you protecting?
                      </label>
                      <textarea
                        rows={4}
                        placeholder="Describe your network environment, current tools, and the biggest security challenge you're facing…"
                        value={form.message}
                        onChange={set("message")}
                        className={`${inputBase} resize-none leading-relaxed`}
                      />
                    </div>

                    {error && (
                      <motion.p
                        initial={{ opacity: 0, y: -4 }}
                        animate={{ opacity: 1, y: 0 }}
                        className="text-xs px-1"
                        style={{ color: "hsl(0 72% 65%)" }}
                      >
                        {error}
                      </motion.p>
                    )}

                    <button
                      type="submit"
                      disabled={submitting}
                      className="w-full flex items-center justify-center gap-2 px-6 py-3.5 rounded-lg text-sm font-semibold transition-all duration-200 disabled:opacity-60"
                      style={{
                        background: "hsl(220 20% 96%)",
                        color: "hsl(222 47% 11%)",
                        letterSpacing: "-0.01em",
                      }}
                      onMouseEnter={(e) => {
                        (e.currentTarget as HTMLButtonElement).style.background = "hsl(220 20% 90%)";
                      }}
                      onMouseLeave={(e) => {
                        (e.currentTarget as HTMLButtonElement).style.background = "hsl(220 20% 96%)";
                      }}
                    >
                      {submitting ? (
                        <>
                          <Loader2 className="w-4 h-4 animate-spin" />
                          Sending...
                        </>
                      ) : (
                        <>
                          Request a Pilot
                          <ArrowRight className="w-4 h-4" />
                        </>
                      )}
                    </button>

                    <p className="text-center text-[11px]" style={{ color: "hsl(220 15% 36%)" }}>
                      No commitment · We'll reply within 24 hours
                    </p>
                  </motion.form>
                )}
              </AnimatePresence>
            </div>
          </motion.div>
        </div>
      </div>
    </section>
  );
};

export default ContactSection;
