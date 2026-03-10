import { Helmet } from "react-helmet-async";
import LandingNavbar from "@/components/landing-v2/layout/LandingNavbar";
import LandingFooter from "@/components/landing-v2/layout/LandingFooter";
import HeroSection from "@/components/landing-v2/sections/HeroSection";
import LogoTicker from "@/components/landing-v2/sections/LogoTicker";
import ProblemSection from "@/components/landing-v2/sections/ProblemSection";
import ComparisonSection from "@/components/landing-v2/sections/ComparisonSection";
import SolutionSection from "@/components/landing-v2/sections/SolutionSection";
import PilotSection from "@/components/landing-v2/sections/PilotSection";
import ResearchSection from "@/components/landing-v2/sections/ResearchSection";
import TeamSection from "@/components/landing-v2/sections/TeamSection";
import FAQSection from "@/components/landing-v2/sections/FAQSection";
import ScrollReveal from "@/components/landing-v2/shared/ScrollReveal";
import { InlineContactForm } from "@/components/landing";

const ContactSection = () => (
  <section id="contact" className="py-24 px-6 bg-cta-bg text-cta-foreground relative overflow-hidden">
    <div
      className="pointer-events-none absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 w-[600px] h-[400px] opacity-[0.06]"
      style={{ background: "radial-gradient(ellipse, hsl(42 78% 60%), transparent 70%)" }}
    />
    <div className="relative mx-auto max-w-2xl text-center">
      <h2 className="text-3xl sm:text-4xl lg:text-5xl font-display font-bold tracking-tight">
        Let&apos;s talk about your network.
      </h2>
      <p className="mt-4 text-cta-muted text-lg">
        Tell us a bit about your environment and we will get back to you within 24 hours.
      </p>
      <div className="mt-10">
        <InlineContactForm />
      </div>
    </div>
  </section>
);

const Index = () => {
  return (
    <>
      <Helmet>
        <title>CyberMesh - AI-Powered Blockchain Security</title>
        <meta
          name="description"
          content="CyberMesh provides enterprise-grade blockchain security with AI-powered threat detection and autonomous response."
        />
      </Helmet>
      <div className="min-h-screen bg-background">
        <LandingNavbar />
        <main>
          <HeroSection />
          <ScrollReveal>
            <LogoTicker />
          </ScrollReveal>
          <ScrollReveal>
            <ProblemSection />
          </ScrollReveal>
          <ScrollReveal>
            <ComparisonSection />
          </ScrollReveal>
          <ScrollReveal>
            <SolutionSection />
          </ScrollReveal>
          <ScrollReveal>
            <PilotSection />
          </ScrollReveal>
          <ScrollReveal>
            <ResearchSection />
          </ScrollReveal>
          <ScrollReveal>
            <TeamSection />
          </ScrollReveal>
          <ScrollReveal>
            <FAQSection />
          </ScrollReveal>
          <ScrollReveal>
            <ContactSection />
          </ScrollReveal>
        </main>
        <LandingFooter />
      </div>
    </>
  );
};

export default Index;
