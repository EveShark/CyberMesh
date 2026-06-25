import { Helmet } from "react-helmet-async";
import LandingNavbar from "@/components/landing-v2/layout/LandingNavbar";
import LandingFooter from "@/components/landing-v2/layout/LandingFooter";
import HeroSection from "@/components/landing-v2/sections/HeroSection";
import LogoTicker from "@/components/landing-v2/sections/LogoTicker";
import ProblemSection from "@/components/landing-v2/sections/ProblemSection";
import WorldviewSection from "@/components/landing-v2/sections/WorldviewSection";
import ComparisonSection from "@/components/landing-v2/sections/ComparisonSection";
import SolutionSection from "@/components/landing-v2/sections/SolutionSection";
import ProductGallerySection from "@/components/landing-v2/sections/ProductGallerySection";
import PilotSection from "@/components/landing-v2/sections/PilotSection";
import ResearchSection from "@/components/landing-v2/sections/ResearchSection";
import TeamSection from "@/components/landing-v2/sections/TeamSection";
import FAQSection from "@/components/landing-v2/sections/FAQSection";
import ScrollReveal from "@/components/landing-v2/shared/ScrollReveal";
import ContactSection from "@/components/landing-v2/sections/ContactSection";

const StatementSection = () => (
  <section className="py-20 px-6" style={{ background: "hsl(222 50% 7%)" }}>
    <div className="mx-auto max-w-6xl">
      <p className="text-4xl sm:text-5xl lg:text-[3.5rem] font-display font-bold text-white leading-[1.1] tracking-tight">
        Detection without enforcement
        <br />
        <span className="italic" style={{ color: "hsl(42 78% 60%)" }}>
          is just expensive noise.
        </span>
      </p>
      <p className="mt-6 text-white/45 text-base leading-relaxed max-w-lg">
        Every SIEM alert, every NDR flag, every EDR event ends the same way: a human has to decide what to do. The moment your attacker is autonomous, that dependency is the breach. CyberMesh ends it.
      </p>
    </div>
  </section>
);

const Index = () => {
  return (
    <>
      <Helmet>
        <title>CyberMesh: The trust layer for autonomous network defense</title>
        <meta
          name="description"
          content="CyberMesh is the trust layer for autonomous network defense. It detects and contains threats at the infrastructure level, validating every decision before it acts, in under a second."
        />
      </Helmet>
      <div className="min-h-screen bg-background">
        <LandingNavbar />
        <main>
          <HeroSection />
          <ScrollReveal>
            <LogoTicker />
          </ScrollReveal>
          <StatementSection />
          <ScrollReveal>
            <ProductGallerySection />
          </ScrollReveal>
          <ScrollReveal>
            <ProblemSection />
          </ScrollReveal>
          <WorldviewSection />
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
