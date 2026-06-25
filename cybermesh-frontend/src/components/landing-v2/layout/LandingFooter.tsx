import { Linkedin } from "lucide-react";

const Footer = () => (
  <footer className="py-10 px-6 border-t border-border bg-background">
    <div className="mx-auto max-w-6xl flex flex-col sm:flex-row items-center justify-between gap-4">
      <div>
        <span className="text-sm font-display font-bold text-primary">CyberMesh</span>
        <p className="text-xs text-muted-foreground mt-0.5">The trust layer for autonomous network defense.</p>
      </div>
      <span className="text-xs text-muted-foreground">
        © 2026 CyberMesh. All rights reserved.
      </span>
      <a href="https://linkedin.com" target="_blank" rel="noopener noreferrer"
        className="text-muted-foreground hover:text-primary transition-colors" aria-label="LinkedIn"
      >
        <Linkedin className="w-5 h-5" />
      </a>
    </div>
  </footer>
);

export default Footer;
