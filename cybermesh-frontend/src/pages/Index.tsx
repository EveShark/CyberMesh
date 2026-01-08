import { Link } from "react-router-dom";
import { Helmet } from "react-helmet-async";
import { 
  ArchitectureDiagram, 
  FAQSection,
  InlineContactForm,
  NavBar,
  ScrollRevealSection,
  SpeedComparison,
  SovereignDifference,
  ThreatLandscape,
  TrustBadges
} from "@/components/landing";
import EmberParticles from "@/components/landing/EmberParticles";
import BentoStats from "@/components/landing/BentoStats";
import { ChevronDown } from "lucide-react";

const Index = () => {

  const scrollToSection = (sectionId: string) => {
    const section = document.getElementById(sectionId);
    if (section) {
      const headerOffset = 80;
      const elementPosition = section.getBoundingClientRect().top;
      const offsetPosition = elementPosition + window.pageYOffset - headerOffset;
      
      window.scrollTo({
        top: offsetPosition,
        behavior: 'smooth'
      });
    }
  };

  return (
    <>
      <Helmet>
        <title>CyberMesh - AI-Powered Blockchain Security</title>
        <meta name="description" content="CyberMesh provides enterprise-grade blockchain security with AI-powered threat detection, real-time monitoring, and decentralized defense architecture." />
        <meta name="keywords" content="blockchain security, AI threat detection, cybersecurity, decentralized security, enterprise security" />
        <meta property="og:title" content="CyberMesh - AI-Powered Blockchain Security" />
        <meta property="og:description" content="Enterprise-grade blockchain security with AI-powered threat detection and real-time monitoring." />
        <meta property="og:type" content="website" />
        <meta name="twitter:card" content="summary_large_image" />
        <meta name="twitter:title" content="CyberMesh - AI-Powered Blockchain Security" />
        <meta name="twitter:description" content="Enterprise-grade blockchain security with AI-powered threat detection and real-time monitoring." />
        <link rel="canonical" href="https://cybermesh.io" />
      </Helmet>
      <div className="min-h-screen bg-background overflow-x-hidden scroll-smooth">
        {/* Navigation */}
        <NavBar demoUrl="/dashboard" />

      {/* Hero Section */}
      <section className="relative min-h-[80vh] md:min-h-[85vh] flex flex-col items-center justify-center px-4 md:px-6 py-16 pt-20">
        {/* Ember particles background */}
        <EmberParticles />
        
        {/* Gradient overlays */}
        <div className="absolute inset-0 bg-gradient-to-b from-background via-transparent to-background pointer-events-none" />
        <div className="absolute inset-0 bg-gradient-to-r from-background/60 via-transparent to-background/60 pointer-events-none" />
        
        <div className="relative z-10 max-w-3xl mx-auto text-center">

          {/* Main headline */}
          <h1 
            className="text-xl sm:text-2xl md:text-3xl lg:text-4xl font-black text-foreground leading-[1.2] mb-4 animate-blur-in tracking-tight px-2"
            style={{ animationDelay: "0.1s" }}
          >
            If your security is centralized,
            <br />
            <span className="text-gradient-ember">your integrity is a single point of failure.</span>
          </h1>

          {/* Sub-headline */}
          <p 
            className="text-xs md:text-sm lg:text-base text-muted-foreground max-w-lg mx-auto mb-6 md:mb-8 animate-blur-in leading-relaxed px-4"
            style={{ animationDelay: "0.2s" }}
          >
            We are building the unassailable foundation for the next decade of autonomous defense.
          </p>

          {/* CTA buttons */}
          <div 
            className="flex flex-col sm:flex-row items-center justify-center gap-2.5 md:gap-3 animate-blur-in px-4"
            style={{ animationDelay: "0.3s" }}
          >
            <Link 
              to="/dashboard" 
              className="group relative overflow-hidden px-5 py-2.5 md:px-6 md:py-3 rounded-full gradient-ember text-primary-foreground font-semibold text-xs md:text-sm transition-all duration-300 hover:scale-105 ember-glow-sm"
            >
              <span className="relative z-10">Explore Demo</span>
              <div className="absolute inset-0 bg-gradient-to-r from-spark to-ember opacity-0 group-hover:opacity-100 transition-opacity duration-300" />
            </Link>
            <button 
              onClick={() => scrollToSection('contact')}
              className="px-5 py-2.5 md:px-6 md:py-3 rounded-full bento-card text-foreground font-semibold text-xs md:text-sm transition-all duration-300 hover:border-ember/40"
            >
              Contact Us
            </button>
          </div>
          
          {/* Quiet enterprise line */}
          <p 
            className="text-[10px] md:text-xs text-muted-foreground/70 mt-4 animate-blur-in"
            style={{ animationDelay: "0.4s" }}
          >
            Currently engaging with a limited number of enterprise design partners.
          </p>
        </div>

        {/* Scroll indicator - Mobile First */}
        <button 
          onClick={() => scrollToSection('speed-comparison')}
          className="absolute bottom-6 md:bottom-8 left-0 right-0 flex justify-center"
          aria-label="Scroll to next section"
        >
          <div className="flex flex-col items-center p-2.5 md:p-3 rounded-full bg-muted/10 border border-muted/20 backdrop-blur-sm hover:bg-muted/20 hover:border-ember/30 transition-all duration-300 group cursor-pointer">
            <div className="flex flex-col items-center gap-0">
              <ChevronDown className="w-4 h-4 md:w-5 md:h-5 text-muted-foreground group-hover:text-ember transition-colors animate-[bounce_2s_ease-in-out_infinite]" />
              <ChevronDown className="w-4 h-4 md:w-5 md:h-5 -mt-2.5 text-muted-foreground/50 group-hover:text-ember/50 transition-colors animate-[bounce_2s_ease-in-out_infinite_0.15s]" />
            </div>
          </div>
        </button>
      </section>

      {/* Speed Comparison Section */}
      <section id="speed-comparison" className="relative px-4 md:px-6 py-10 md:py-14">
        <div className="section-divider mb-8 md:mb-10" />
        <ScrollRevealSection className="max-w-3xl mx-auto">
          <div className="text-center mb-6 md:mb-8">
            <span className="inline-block px-2.5 py-1 rounded-full text-[9px] md:text-[10px] font-semibold uppercase tracking-widest text-ember bg-ember/10 border border-ember/20 mb-3">
              The Problem
            </span>
            <h2 className="text-lg md:text-xl lg:text-2xl font-bold text-foreground mb-2 leading-tight">
              The Autonomous Threat is Here
            </h2>
            <p className="text-xs md:text-sm text-muted-foreground max-w-md mx-auto leading-relaxed">
              Machine-speed attacks are collapsing the human-in-the-loop SOC model.
            </p>
          </div>
          
          <SpeedComparison />
        </ScrollRevealSection>
      </section>

      {/* Stats Section */}
      <section className="relative px-4 md:px-6 py-10 md:py-14">
        <ScrollRevealSection className="max-w-4xl mx-auto">
          <div className="text-center mb-6 md:mb-8">
            <span className="inline-block px-2.5 py-1 rounded-full text-[9px] md:text-[10px] font-semibold uppercase tracking-widest text-spark bg-spark/10 border border-spark/20 mb-3">
              The Reality
            </span>
            <h2 className="text-lg md:text-xl lg:text-2xl font-bold text-foreground mb-2 leading-tight">
              The SOC is Overwhelmed
            </h2>
            <p className="text-xs md:text-sm text-muted-foreground max-w-md mx-auto leading-relaxed">
              Traditional security operations cannot scale to meet the challenge.
            </p>
          </div>
          
          <BentoStats />
        </ScrollRevealSection>
      </section>

      {/* Threat Landscape Section */}
      <section className="relative px-4 md:px-6 py-10 md:py-14">
        <ScrollRevealSection className="max-w-3xl mx-auto" direction="scale">
          <ThreatLandscape />
        </ScrollRevealSection>
      </section>

      {/* Sovereign Difference Section */}
      <section className="relative px-4 md:px-6 py-10 md:py-14">
        <ScrollRevealSection className="max-w-4xl mx-auto">
          <div className="text-center mb-6 md:mb-8">
            <span className="inline-block px-2.5 py-1 rounded-full text-[9px] md:text-[10px] font-semibold uppercase tracking-widest text-frost bg-frost/10 border border-frost/20 mb-3">
              The Solution
            </span>
            <h2 className="text-lg md:text-xl lg:text-2xl font-bold text-foreground mb-2 leading-tight">
              The Sovereign Difference
            </h2>
            <p className="text-xs md:text-sm text-muted-foreground max-w-md mx-auto leading-relaxed">
              A new architecture for unassailable, decentralized security.
            </p>
          </div>
          
          <SovereignDifference />
        </ScrollRevealSection>
      </section>

      {/* Architecture Diagram */}
      <section className="relative px-4 md:px-6 py-10 md:py-14">
        <ScrollRevealSection className="max-w-3xl mx-auto" direction="up">
          <div className="text-center mb-6 md:mb-8">
            <span className="inline-block px-2.5 py-1 rounded-full text-[9px] md:text-[10px] font-semibold uppercase tracking-widest text-accent bg-accent/10 border border-accent/20 mb-3">
              The Architecture
            </span>
            <h2 className="text-lg md:text-xl lg:text-2xl font-bold text-foreground mb-2 leading-tight">
              Three Layers of Defense
            </h2>
            <p className="text-xs md:text-sm text-muted-foreground max-w-md mx-auto leading-relaxed">
              Autonomous threat detection, consensus verification, and immutable logging.
            </p>
          </div>
          
          <ArchitectureDiagram />
        </ScrollRevealSection>
      </section>

      {/* Trust Badges */}
      <section className="relative px-4 md:px-6 py-6 md:py-10">
        <ScrollRevealSection className="max-w-4xl mx-auto" direction="scale">
          <TrustBadges />
        </ScrollRevealSection>
      </section>

      {/* FAQ Section */}
      <section className="relative px-4 md:px-6 py-10 md:py-14">
        <ScrollRevealSection className="max-w-2xl mx-auto">
          <FAQSection />
        </ScrollRevealSection>
      </section>

      {/* Contact Section */}
      <section id="contact" className="relative px-4 md:px-6 py-10 md:py-14">
        <ScrollRevealSection className="max-w-xl mx-auto text-center" direction="scale">
          <div className="bento-card rounded-xl p-5 md:p-6 lg:p-8 relative overflow-hidden border-ember/20">
            {/* Background gradient */}
            <div className="absolute inset-0 bg-gradient-to-br from-ember/5 via-transparent to-spark/5" />
            
            <div className="relative">
              <span className="inline-block px-2.5 py-1 rounded-full text-[9px] md:text-[10px] font-semibold uppercase tracking-widest text-ember bg-ember/10 border border-ember/20 mb-3">
                Get In Touch
              </span>
              <h2 className="text-lg md:text-xl font-bold text-foreground mb-2 md:mb-3">
                Ready to Get Started?
              </h2>
              <p className="text-xs md:text-sm text-muted-foreground mb-5 md:mb-6 max-w-sm mx-auto">
                We're currently engaging with select enterprise partners. Tell us about your security needs.
              </p>
              
              {/* Inline contact form */}
              <InlineContactForm />
              
              <div className="mt-5 flex items-center justify-center">
                <Link 
                  to="/dashboard" 
                  className="group relative overflow-hidden px-5 py-2.5 rounded-full gradient-ember text-primary-foreground font-semibold text-xs md:text-sm transition-all duration-300 hover:scale-105 ember-glow-sm"
                >
                  <span className="relative z-10">Explore Demo</span>
                  <div className="absolute inset-0 bg-gradient-to-r from-spark to-ember opacity-0 group-hover:opacity-100 transition-opacity duration-300" />
                </Link>
              </div>
            </div>
          </div>
        </ScrollRevealSection>
      </section>

      {/* Footer */}
      <footer className="relative px-4 md:px-6 py-6 md:py-8">
        <div className="section-divider mb-6" />
        <div className="max-w-4xl mx-auto flex flex-col md:flex-row items-center justify-center gap-3">
          <p className="text-[10px] text-muted-foreground">
            © 2025 CyberMesh
          </p>
        </div>
      </footer>


      </div>
    </>
  );
};

export default Index;
