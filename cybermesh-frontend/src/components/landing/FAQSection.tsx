import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from "@/components/ui/accordion";
import { MessageCircleQuestion } from "lucide-react";

const faqs = [
  {
    question: "What is CyberMesh?",
    answer: "CyberMesh is a distributed security decision system that uses independent detectors and consensus-based enforcement to eliminate single-point failures in threat response."
  },
  {
    question: "How is this different from XDR or SOC platforms?",
    answer: "Traditional platforms centralize detection and decisions in one control plane. CyberMesh requires agreement across independent nodes before enforcement, making decisions resilient to model errors, compromises, and silent corruption."
  },
  {
    question: "Does this slow down security operations?",
    answer: "CyberMesh separates detection speed from enforcement correctness. Only high-impact actions require consensus. Fast detection paths remain fast."
  },
  {
    question: "Why is CyberMesh in stealth?",
    answer: "We are validating enforcement thresholds, operational workflows, and compliance requirements with a small number of enterprise design partners before broader release."
  },
  {
    question: "Is the system operational today?",
    answer: "Yes. The core architecture is live in controlled deployments with select partners."
  },
  {
    question: "Who is CyberMesh designed for?",
    answer: "CyberMesh is built for regulated enterprises and security teams that require auditability, decision traceability, and resilience against silent failure."
  },
  {
    question: "How can organizations engage during stealth?",
    answer: "We selectively engage with organizations suited for early deployments. Qualified teams can request access to technical material and private discussions."
  },
];

const FAQSection = () => {
  return (
    <div className="bento-card rounded-xl p-5 md:p-6 lg:p-8 border-ember/10">
      <div className="text-center mb-8">
        <span className="inline-block px-2.5 py-1 rounded-full text-[9px] md:text-[10px] font-semibold uppercase tracking-widest text-muted-foreground bg-muted/50 border border-border/50 mb-3">
          FAQ
        </span>
        <h2 className="text-lg md:text-xl font-bold text-foreground">
          Frequently Asked Questions
        </h2>
      </div>
      
      <Accordion type="single" collapsible className="w-full space-y-2">
        {faqs.map((faq, index) => (
          <AccordionItem 
            key={index} 
            value={`item-${index}`}
            className="border border-border/30 rounded-lg px-4 bg-secondary/20 data-[state=open]:bg-secondary/40 transition-colors"
          >
            <AccordionTrigger className="text-left text-sm md:text-base text-foreground hover:text-ember transition-colors py-4 gap-3 [&>svg]:text-ember/60 [&>svg]:shrink-0">
              <span className="flex items-center gap-3">
                <MessageCircleQuestion className="w-4 h-4 text-ember/70 shrink-0" />
                <span>{faq.question}</span>
              </span>
            </AccordionTrigger>
            <AccordionContent className="text-xs md:text-sm text-muted-foreground leading-relaxed pb-4 pl-7">
              {faq.answer}
            </AccordionContent>
          </AccordionItem>
        ))}
      </Accordion>
    </div>
  );
};

export default FAQSection;
