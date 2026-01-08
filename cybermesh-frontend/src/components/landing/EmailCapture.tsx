import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useToast } from "@/hooks/common/use-toast";
import { mailApi, emailCaptureSchema } from "@/lib/mail-api";

const EmailCapture = () => {
  const [email, setEmail] = useState("");
  const [honeypot, setHoneypot] = useState(""); // Honeypot field - should remain empty
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [isSubmitted, setIsSubmitted] = useState(false);
  const { toast } = useToast();

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    
    // Bot detection: if honeypot field is filled, silently reject
    if (honeypot) {
      // Fake success to not alert bots
      setIsSubmitted(true);
      return;
    }
    
    // Validate email with zod
    const result = emailCaptureSchema.safeParse(email);
    if (!result.success) {
      toast({
        title: "Invalid email",
        description: result.error.errors[0].message,
        variant: "destructive",
      });
      return;
    }

    setIsSubmitting(true);
    
    try {
      await mailApi.sendEmailCapture(result.data, "landing");
      
      setIsSubmitted(true);
      setEmail("");
      toast({
        title: "Request received",
        description: "We'll be in touch with qualified parties.",
      });
    } catch (error) {
      console.error("Waitlist submission error:", error);
      
      // Check for rate limiting
      if (error instanceof Error && error.message.includes("rate limit")) {
        toast({
          title: "Too many requests",
          description: "Please wait a moment before trying again.",
          variant: "destructive",
        });
      } else {
        toast({
          title: "Something went wrong",
          description: error instanceof Error ? error.message : "Please try again later.",
          variant: "destructive",
        });
      }
    } finally {
      setIsSubmitting(false);
    }
  };

  if (isSubmitted) {
    return (
      <div className="text-center py-4 glass-frost rounded-lg px-6">
        <p className="text-primary font-medium">Request submitted.</p>
        <p className="text-muted-foreground text-sm mt-1">
          We review all inquiries. Qualified parties will receive the technical whitepaper.
        </p>
      </div>
    );
  }

  return (
    <form onSubmit={handleSubmit} className="flex flex-col sm:flex-row gap-3 w-full max-w-md mx-auto">
      {/* Honeypot field - hidden from users, visible to bots */}
      <input
        type="text"
        name="website"
        value={honeypot}
        onChange={(e) => setHoneypot(e.target.value)}
        className="absolute -left-[9999px] opacity-0 pointer-events-none"
        tabIndex={-1}
        autoComplete="off"
        aria-hidden="true"
      />
      
      <Input
        type="email"
        placeholder="Enter your email"
        value={email}
        onChange={(e) => setEmail(e.target.value)}
        className="flex-1 glass border-border/50 text-foreground placeholder:text-muted-foreground focus:border-primary focus:ring-primary"
        disabled={isSubmitting}
      />
      <Button
        type="submit"
        disabled={isSubmitting}
        className="gradient-frost text-primary-foreground font-medium px-6 transition-all duration-200 hover:shadow-lg hover:shadow-frost/20 frost-glow"
      >
        {isSubmitting ? "Submitting..." : "Request Whitepaper"}
      </Button>
    </form>
  );
};

export default EmailCapture;
