import { useState, useCallback, useEffect } from "react";
import { z } from "zod";
import { mailApi } from "@/lib/mail-api";
import { toast } from "@/hooks/common/use-toast";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Skeleton } from "@/components/ui/skeleton";
import { Send, Loader2, Check, AlertCircle, PartyPopper } from "lucide-react";
import { cn } from "@/lib/utils";

const NAME_MAX = 100;
const MESSAGE_MAX = 1000;

const contactSchema = z.object({
  name: z.string().trim()
    .min(1, "Name is required")
    .max(NAME_MAX, `Name must be less than ${NAME_MAX} characters`),
  email: z.string().trim()
    .min(1, "Email is required")
    .email("Please enter a valid email (e.g., name@example.com)")
    .max(255, "Email must be less than 255 characters")
    .refine(
      (email) => /^[^\s@]+@[^\s@]+\.[^\s@]{2,}$/.test(email),
      "Email must have a valid domain (e.g., .com, .org)"
    ),
  company: z.string().trim().max(100, "Company must be less than 100 characters").optional(),
  message: z.string().trim()
    .min(1, "Message is required")
    .min(10, "Please provide a more detailed message (at least 10 characters)")
    .max(MESSAGE_MAX, `Message must be less than ${MESSAGE_MAX} characters`),
});

type ContactFormData = z.infer<typeof contactSchema>;

type FieldErrors = {
  name?: string;
  email?: string;
  message?: string;
};

type TouchedFields = {
  name: boolean;
  email: boolean;
  message: boolean;
};

const ConfettiParticle = ({ delay, left }: { delay: number; left: number }) => (
  <div
    className="absolute w-2 h-2 rounded-full animate-confetti"
    style={{
      left: `${left}%`,
      animationDelay: `${delay}ms`,
      backgroundColor: `hsl(${Math.random() * 360}, 70%, 60%)`,
    }}
  />
);

const FormSkeleton = () => (
  <div className="w-full max-w-md mx-auto space-y-4">
    <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
      <Skeleton className="h-10 w-full bg-muted/30" />
      <Skeleton className="h-10 w-full bg-muted/30" />
    </div>
    <Skeleton className="h-10 w-full bg-muted/30" />
    <Skeleton className="h-[100px] w-full bg-muted/30" />
    <Skeleton className="h-10 w-full bg-muted/30" />
  </div>
);

const InlineContactForm = () => {
  const [isLoading, setIsLoading] = useState(true);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [showSuccess, setShowSuccess] = useState(false);
  const [honeypot, setHoneypot] = useState(""); // Honeypot field - should stay empty
  const [formData, setFormData] = useState<ContactFormData>({
    name: "",
    email: "",
    company: "",
    message: "",
  });
  const [errors, setErrors] = useState<FieldErrors>({});
  const [touched, setTouched] = useState<TouchedFields>({
    name: false,
    email: false,
    message: false,
  });

  // Simulate initial loading state
  useEffect(() => {
    const timer = setTimeout(() => setIsLoading(false), 500);
    return () => clearTimeout(timer);
  }, []);

  const validateField = useCallback((field: keyof FieldErrors, value: string): string | undefined => {
    try {
      if (field === "name") {
        z.string().trim()
          .min(1, "Name is required")
          .max(NAME_MAX, `Name must be less than ${NAME_MAX} characters`)
          .parse(value);
      } else if (field === "email") {
        const trimmed = value.trim();
        if (!trimmed) return "Email is required";
        if (!trimmed.includes("@")) return "Email must contain @ symbol";
        if (!/^[^\s@]+@[^\s@]+$/.test(trimmed)) return "Invalid email format";
        if (!/^[^\s@]+@[^\s@]+\.[^\s@]{2,}$/.test(trimmed)) return "Email must have a valid domain (e.g., .com, .org)";
        z.string().max(255, "Email must be less than 255 characters").parse(trimmed);
      } else if (field === "message") {
        const trimmed = value.trim();
        if (!trimmed) return "Message is required";
        if (trimmed.length < 10) return "Please provide a more detailed message (at least 10 characters)";
        if (trimmed.length > MESSAGE_MAX) return `Message must be less than ${MESSAGE_MAX} characters`;
      }
      return undefined;
    } catch (error) {
      if (error instanceof z.ZodError) {
        return error.errors[0].message;
      }
      return "Invalid input";
    }
  }, []);

  const handleFieldChange = (field: keyof ContactFormData, value: string) => {
    setFormData(prev => ({ ...prev, [field]: value }));
    
    // Only validate if field has been touched
    if (field in touched && touched[field as keyof TouchedFields]) {
      const error = validateField(field as keyof FieldErrors, value);
      setErrors(prev => ({ ...prev, [field]: error }));
    }
  };

  const handleFieldBlur = (field: keyof FieldErrors) => {
    setTouched(prev => ({ ...prev, [field]: true }));
    const error = validateField(field, formData[field]);
    setErrors(prev => ({ ...prev, [field]: error }));
  };

  const isFieldValid = (field: keyof FieldErrors): boolean => {
    return touched[field] && !errors[field] && formData[field].trim().length > 0;
  };

  const isFieldInvalid = (field: keyof FieldErrors): boolean => {
    return touched[field] && !!errors[field];
  };

  const getCharacterCountColor = (current: number, max: number) => {
    const ratio = current / max;
    if (ratio >= 1) return "text-destructive";
    if (ratio >= 0.9) return "text-amber-500";
    return "text-muted-foreground/60";
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    
    // Honeypot check - if filled, silently reject (bot detected)
    if (honeypot) {
      console.log('Bot detected via honeypot');
      // Fake success to not alert bots
      setShowSuccess(true);
      setTimeout(() => setShowSuccess(false), 3000);
      return;
    }
    
    setIsSubmitting(true);

    try {
      const validatedData = contactSchema.parse(formData);

      // Send email via mail gateway
      await mailApi.sendContactForm(validatedData);

      // Show success animation
      setShowSuccess(true);
      setTimeout(() => setShowSuccess(false), 3000);

      toast({
        title: "🎉 Message sent!",
        description: "We'll get back to you within 24 hours.",
      });

      setFormData({ name: "", email: "", company: "", message: "" });
      setErrors({});
      setTouched({ name: false, email: false, message: false });
    } catch (error) {
      if (error instanceof z.ZodError) {
        const firstError = error.errors[0];
        const errorMessage = firstError.message;
        
        // Set field-specific error
        if (firstError.path.length > 0) {
          const fieldName = firstError.path[0] as keyof FieldErrors;
          setErrors(prev => ({ ...prev, [fieldName]: errorMessage }));
          setTouched(prev => ({ ...prev, [fieldName]: true }));
        }
        
        toast({
          title: "Validation error",
          description: errorMessage,
          variant: "destructive",
        });
      } else {
        const errorMessage = error instanceof Error ? error.message : "Please try again later.";
        
        toast({
          title: "Something went wrong",
          description: errorMessage,
          variant: "destructive",
        });
      }
    } finally {
      setIsSubmitting(false);
    }
  };

  // Generate confetti particles
  const confettiParticles = Array.from({ length: 30 }, (_, i) => ({
    id: i,
    delay: Math.random() * 500,
    left: Math.random() * 100,
  }));

  // Show skeleton while loading
  if (isLoading) {
    return <FormSkeleton />;
  }

  return (
    <div className="relative">
      {/* Confetti Animation */}
      {showSuccess && (
        <div className="absolute inset-0 overflow-hidden pointer-events-none z-10">
          {confettiParticles.map((particle) => (
            <ConfettiParticle key={particle.id} delay={particle.delay} left={particle.left} />
          ))}
        </div>
      )}

      {/* Success Overlay */}
      {showSuccess && (
        <div className="absolute inset-0 flex items-center justify-center bg-background/80 backdrop-blur-sm rounded-lg z-20 animate-fade-in">
          <div className="text-center space-y-2">
            <PartyPopper className="w-12 h-12 mx-auto text-ember animate-bounce" />
            <p className="text-lg font-semibold text-foreground">Thank you!</p>
            <p className="text-sm text-muted-foreground">We'll be in touch soon.</p>
          </div>
        </div>
      )}

      <form onSubmit={handleSubmit} className="w-full max-w-md mx-auto space-y-4" id="contact-form">
        {/* Honeypot field - hidden from users, visible to bots */}
        <div className="absolute -left-[9999px] opacity-0 h-0 overflow-hidden" aria-hidden="true">
          <label htmlFor="website">Website</label>
          <input
            type="text"
            id="website"
            name="website"
            value={honeypot}
            onChange={(e) => setHoneypot(e.target.value)}
            tabIndex={-1}
            autoComplete="off"
          />
        </div>

        <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
          {/* Name Field */}
          <div className="space-y-1">
            <div className="relative">
              <Input
                placeholder="Your name *"
                value={formData.name}
                onChange={(e) => handleFieldChange("name", e.target.value)}
                onBlur={() => handleFieldBlur("name")}
                maxLength={NAME_MAX + 10}
                className={cn(
                  "bg-background/50 border-muted/30 focus:border-ember/50 placeholder:text-muted-foreground/50 pr-10 transition-all duration-200",
                  isFieldValid("name") && "border-green-500/50 focus:border-green-500",
                  isFieldInvalid("name") && "border-destructive/50 focus:border-destructive"
                )}
                required
              />
              <div className="absolute right-3 top-1/2 -translate-y-1/2">
                {isFieldValid("name") && <Check className="w-4 h-4 text-green-500" />}
                {isFieldInvalid("name") && <AlertCircle className="w-4 h-4 text-destructive" />}
              </div>
            </div>
            <div className="flex justify-between items-center px-1">
              {isFieldInvalid("name") ? (
                <p className="text-xs text-destructive">{errors.name}</p>
              ) : (
                <span />
              )}
              <span className={cn("text-xs", getCharacterCountColor(formData.name.length, NAME_MAX))}>
                {formData.name.length}/{NAME_MAX}
              </span>
            </div>
          </div>

          {/* Email Field */}
          <div className="space-y-1">
            <div className="relative">
              <Input
                type="email"
                placeholder="Email address *"
                value={formData.email}
                onChange={(e) => handleFieldChange("email", e.target.value)}
                onBlur={() => handleFieldBlur("email")}
                className={cn(
                  "bg-background/50 border-muted/30 focus:border-ember/50 placeholder:text-muted-foreground/50 pr-10 transition-all duration-200",
                  isFieldValid("email") && "border-green-500/50 focus:border-green-500",
                  isFieldInvalid("email") && "border-destructive/50 focus:border-destructive"
                )}
                required
              />
              <div className="absolute right-3 top-1/2 -translate-y-1/2">
                {isFieldValid("email") && <Check className="w-4 h-4 text-green-500" />}
                {isFieldInvalid("email") && <AlertCircle className="w-4 h-4 text-destructive" />}
              </div>
            </div>
            {isFieldInvalid("email") && (
              <p className="text-xs text-destructive px-1">{errors.email}</p>
            )}
          </div>
        </div>

        {/* Company Field (Optional) */}
        <Input
          placeholder="Company (optional)"
          value={formData.company}
          onChange={(e) => handleFieldChange("company", e.target.value)}
          className="bg-background/50 border-muted/30 focus:border-ember/50 placeholder:text-muted-foreground/50"
        />

        {/* Message Field */}
        <div className="space-y-1">
          <div className="relative">
            <Textarea
              placeholder="How can we help you? *"
              value={formData.message}
              onChange={(e) => handleFieldChange("message", e.target.value)}
              onBlur={() => handleFieldBlur("message")}
              maxLength={MESSAGE_MAX + 50}
              className={cn(
                "bg-background/50 border-muted/30 focus:border-ember/50 placeholder:text-muted-foreground/50 min-h-[100px] resize-none pr-10 transition-all duration-200",
                isFieldValid("message") && "border-green-500/50 focus:border-green-500",
                isFieldInvalid("message") && "border-destructive/50 focus:border-destructive"
              )}
              required
            />
            <div className="absolute right-3 top-3">
              {isFieldValid("message") && <Check className="w-4 h-4 text-green-500" />}
              {isFieldInvalid("message") && <AlertCircle className="w-4 h-4 text-destructive" />}
            </div>
          </div>
          <div className="flex justify-between items-center px-1">
            {isFieldInvalid("message") ? (
              <p className="text-xs text-destructive">{errors.message}</p>
            ) : (
              <span />
            )}
            <span className={cn("text-xs", getCharacterCountColor(formData.message.length, MESSAGE_MAX))}>
              {formData.message.length}/{MESSAGE_MAX}
            </span>
          </div>
        </div>

        <Button
          type="submit"
          disabled={isSubmitting || showSuccess}
          className="w-full bg-gradient-to-r from-spark to-ember hover:from-ember hover:to-spark text-white font-semibold transition-all duration-300"
        >
          {isSubmitting ? (
            <>
              <Loader2 className="w-4 h-4 mr-2 animate-spin" />
              Sending...
            </>
          ) : (
            <>
              <Send className="w-4 h-4 mr-2" />
              Send Message
            </>
          )}
        </Button>
      </form>
    </div>
  );
};

export default InlineContactForm;
