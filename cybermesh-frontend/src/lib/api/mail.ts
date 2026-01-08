/**
 * Mail Gateway API Client
 * 
 * Standalone mail API for contact forms. Uses the mail gateway edge function
 * which supports Zoho (ZeptoMail) and Google Workspace (Gmail API) providers.
 * 
 * All email provider credentials stay server-side - frontend just posts form data.
 */

import { z } from "zod";

// Validation schemas
export const contactFormSchema = z.object({
  name: z.string().trim().min(1, "Name is required").max(100, "Name is too long"),
  email: z.string().trim().email("Please enter a valid email").max(255, "Email is too long"),
  company: z.string().trim().max(100, "Company name is too long").optional(),
  message: z.string().trim().min(1, "Message is required").max(1000, "Message is too long"),
});

export const emailCaptureSchema = z.string().trim().email("Please enter a valid email address").max(255, "Email is too long");

export type ContactFormData = z.infer<typeof contactFormSchema>;

// Error type for mail API errors
export class MailApiError extends Error {
  constructor(
    message: string,
    public status: number,
    public isRateLimited: boolean = false
  ) {
    super(message);
    this.name = "MailApiError";
  }
}

/**
 * Get the mail gateway URL from environment or construct from Supabase URL
 */
function getMailGatewayUrl(): string {
  // Use Supabase edge function URL
  const supabaseUrl = import.meta.env.VITE_SUPABASE_URL;
  if (!supabaseUrl) {
    throw new Error("VITE_SUPABASE_URL environment variable is not set");
  }
  return `${supabaseUrl}/functions/v1/send-contact-email`;
}

/**
 * Send a contact form submission to the mail gateway
 */
export async function sendContactForm(data: ContactFormData): Promise<{ success: boolean }> {
  const url = getMailGatewayUrl();
  const publishableKey = import.meta.env.VITE_SUPABASE_PUBLISHABLE_KEY;

  if (!publishableKey) {
    throw new Error("VITE_SUPABASE_PUBLISHABLE_KEY environment variable is not set");
  }

  try {
    const response = await fetch(url, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "apikey": publishableKey,
        "Authorization": `Bearer ${publishableKey}`,
      },
      body: JSON.stringify(data),
    });

    if (response.status === 429) {
      throw new MailApiError(
        "Too many requests. Please wait a moment before trying again.",
        429,
        true
      );
    }

    if (!response.ok) {
      const errorData = await response.json().catch(() => ({}));
      throw new MailApiError(
        errorData.error || `Request failed with status ${response.status}`,
        response.status
      );
    }

    return { success: true };
  } catch (error) {
    if (error instanceof MailApiError) {
      throw error;
    }

    // Network error
    if (error instanceof TypeError && error.message.includes("fetch")) {
      throw new MailApiError("Network error - please check your connection", 0);
    }

    throw new MailApiError(
      error instanceof Error ? error.message : "Unknown error occurred",
      0
    );
  }
}

/**
 * Send an email capture (waitlist signup) to the mail gateway
 * This sends a welcome/confirmation email instead of storing in DB
 */
export async function sendEmailCapture(email: string, source: string = "landing"): Promise<{ success: boolean }> {
  // For email capture, we use the same endpoint but with minimal data
  // The mail gateway will send a confirmation email
  return sendContactForm({
    name: "Waitlist Subscriber",
    email,
    message: `Whitepaper request from ${source}`,
  });
}

export const mailApi = {
  sendContactForm,
  sendEmailCapture,
  schemas: {
    contactForm: contactFormSchema,
    emailCapture: emailCaptureSchema,
  },
};

export default mailApi;
