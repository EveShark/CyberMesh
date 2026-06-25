const partners = [
  "AWS", "Kubernetes", "CrowdStrike", "Palo Alto Networks",
  "Fortinet", "Microsoft Azure", "Google Cloud", "Cisco",
  "Darktrace", "Splunk",
];

const LogoTicker = () => {
  const doubled = [...partners, ...partners];

  return (
    <section className="py-16 px-6 overflow-hidden border-t border-border">
      <p className="text-center text-[11px] font-semibold uppercase tracking-[0.22em] text-muted-foreground mb-10">
        Integrates with your existing stack
      </p>

      <div className="relative max-w-5xl mx-auto">
        <div className="pointer-events-none absolute left-0 top-0 bottom-0 w-24 z-10"
          style={{ background: "linear-gradient(to right, hsl(var(--background)), transparent)" }}
        />
        <div className="pointer-events-none absolute right-0 top-0 bottom-0 w-24 z-10"
          style={{ background: "linear-gradient(to left, hsl(var(--background)), transparent)" }}
        />

        <div className="overflow-hidden">
          <div className="flex items-center gap-16 animate-ticker w-max">
            {doubled.map((name, i) => (
              <span
                key={`${name}-${i}`}
                className="flex-shrink-0 text-sm font-medium text-muted-foreground/60 whitespace-nowrap hover:text-muted-foreground transition-colors duration-200"
              >
                {name}
              </span>
            ))}
          </div>
        </div>
      </div>
    </section>
  );
};

export default LogoTicker;
