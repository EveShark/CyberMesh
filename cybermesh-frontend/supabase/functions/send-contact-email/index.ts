import { serve } from "https://deno.land/std@0.190.0/http/server.ts";

import {
  generateRequestId,
  getClientIdentifier,
  checkRateLimit,
  getResponseHeaders,
  createRateLimitResponse,
  createErrorResponse,
  logRequest,
  logResponse,
  getCorsHeaders,
  securityHeaders,
} from "../_shared/security.ts";

import { sendViaSMTP, getSMTPConfig } from "./smtp.ts";

const FUNCTION_NAME = "send-contact-email";

// Stricter rate limit for contact form: 5 requests per minute
const CONTACT_RATE_LIMIT = { maxRequests: 5, windowMs: 60 * 1000 };

// Max request body size (10KB)
const MAX_BODY_SIZE = 10 * 1024;

interface ContactEmailRequest {
  name: string;
  email: string;
  company?: string;
  message: string;
}

// Format date for email template
function formatDate(): string {
  const now = new Date();
  return now.toLocaleDateString("en-US", {
    year: "numeric",
    month: "long",
    day: "numeric",
  }) + " at " + now.toLocaleTimeString("en-US", {
    hour: "numeric",
    minute: "2-digit",
    hour12: true,
  });
}

// Escape HTML to prevent XSS
function escapeHtml(text: string): string {
  const htmlEntities: Record<string, string> = {
    "&": "&amp;",
    "<": "&lt;",
    ">": "&gt;",
    '"': "&quot;",
    "'": "&#039;",
    "/": "&#x2F;",
    "`": "&#x60;",
    "=": "&#x3D;",
  };
  return text.replace(/[&<>"'`=/]/g, (char) => htmlEntities[char] || char);
}

// Check for script injection patterns
function containsScriptInjection(text: string): boolean {
  const lowerText = text.toLowerCase();
  const dangerousPatterns = [
    /<script/i,
    /javascript:/i,
    /on\w+\s*=/i,
    /data:/i,
    /<iframe/i,
    /<object/i,
    /<embed/i,
    /<svg.*onload/i,
    /expression\s*\(/i,
    /url\s*\(\s*['"]?\s*javascript/i,
  ];
  return dangerousPatterns.some(pattern => pattern.test(lowerText));
}

// Validate input with strict rules
function validateInput(data: unknown): { valid: boolean; error?: string; sanitized?: ContactEmailRequest } {
  if (!data || typeof data !== 'object') {
    return { valid: false, error: "Invalid request body" };
  }
  
  const { name, email, company, message } = data as Record<string, unknown>;
  
  // Validate name
  if (typeof name !== 'string' || name.trim().length === 0) {
    return { valid: false, error: "Name is required" };
  }
  if (name.length > 100) {
    return { valid: false, error: "Name must be less than 100 characters" };
  }
  if (containsScriptInjection(name)) {
    return { valid: false, error: "Invalid characters in name" };
  }
  
  // Validate email
  if (typeof email !== 'string' || email.trim().length === 0) {
    return { valid: false, error: "Email is required" };
  }
  if (email.length > 254) {
    return { valid: false, error: "Email must be less than 254 characters" };
  }
  const emailRegex = /^[a-zA-Z0-9.!#$%&'*+/=?^_`{|}~-]+@[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(?:\.[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$/;
  if (!emailRegex.test(email.trim())) {
    return { valid: false, error: "Invalid email format" };
  }
  
  // Validate company (optional)
  if (company !== undefined && company !== null) {
    if (typeof company !== 'string') {
      return { valid: false, error: "Company must be a string" };
    }
    if (company.length > 100) {
      return { valid: false, error: "Company must be less than 100 characters" };
    }
    if (containsScriptInjection(company)) {
      return { valid: false, error: "Invalid characters in company" };
    }
  }
  
  // Validate message
  if (typeof message !== 'string' || message.trim().length === 0) {
    return { valid: false, error: "Message is required" };
  }
  if (message.length > 5000) {
    return { valid: false, error: "Message must be less than 5000 characters" };
  }
  if (containsScriptInjection(message)) {
    return { valid: false, error: "Invalid characters in message" };
  }
  
  return {
    valid: true,
    sanitized: {
      name: name.trim(),
      email: email.trim().toLowerCase(),
      company: company ? (company as string).trim() : undefined,
      message: message.trim(),
    },
  };
}

// Generate professional HTML email template
function generateEmailTemplate(data: ContactEmailRequest): string {
  const { name, email, company, message } = data;
  const formattedDate = formatDate();
  
  return `
<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>New CyberMesh Inquiry</title>
</head>
<body style="margin: 0; padding: 0; font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif; background-color: #0a0a0a; color: #ffffff;">
  <table width="100%" cellpadding="0" cellspacing="0" style="background-color: #0a0a0a; padding: 40px 20px;">
    <tr>
      <td align="center">
        <table width="600" cellpadding="0" cellspacing="0" style="background-color: #111111; border-radius: 12px; overflow: hidden; border: 1px solid #222;">
          <!-- Header -->
          <tr>
            <td style="background: linear-gradient(135deg, #ff4500 0%, #ff6b35 100%); padding: 30px; text-align: center;">
              <h1 style="margin: 0; color: #ffffff; font-size: 24px; font-weight: 600;">
                NEW CYBERMESH INQUIRY
              </h1>
            </td>
          </tr>
          
          <!-- Contact Info -->
          <tr>
            <td style="padding: 30px;">
              <table width="100%" cellpadding="0" cellspacing="0">
                <tr>
                  <td style="padding-bottom: 20px;">
                    <p style="margin: 0 0 8px 0; color: #888; font-size: 12px; text-transform: uppercase; letter-spacing: 1px;">From</p>
                    <p style="margin: 0; color: #ffffff; font-size: 18px; font-weight: 600;">${escapeHtml(name)}</p>
                  </td>
                </tr>
                <tr>
                  <td style="padding-bottom: 20px;">
                    <p style="margin: 0 0 8px 0; color: #888; font-size: 12px; text-transform: uppercase; letter-spacing: 1px;">Email</p>
                    <p style="margin: 0;">
                      <a href="mailto:${escapeHtml(email)}" style="color: #ff6b35; font-size: 16px; text-decoration: none;">${escapeHtml(email)}</a>
                    </p>
                  </td>
                </tr>
                ${company ? `
                <tr>
                  <td style="padding-bottom: 20px;">
                    <p style="margin: 0 0 8px 0; color: #888; font-size: 12px; text-transform: uppercase; letter-spacing: 1px;">Company</p>
                    <p style="margin: 0; color: #ffffff; font-size: 16px;">${escapeHtml(company)}</p>
                  </td>
                </tr>
                ` : ""}
              </table>
            </td>
          </tr>
          
          <!-- Message Section -->
          <tr>
            <td style="padding: 0 30px 30px 30px;">
              <div style="border-top: 1px solid #333; padding-top: 20px;">
                <p style="margin: 0 0 12px 0; color: #888; font-size: 12px; text-transform: uppercase; letter-spacing: 1px;">Message</p>
                <div style="background-color: #1a1a1a; border-radius: 8px; padding: 20px; border-left: 3px solid #ff4500;">
                  <p style="margin: 0; color: #e0e0e0; font-size: 15px; line-height: 1.6; white-space: pre-wrap;">${escapeHtml(message)}</p>
                </div>
              </div>
            </td>
          </tr>
          
          <!-- Footer -->
          <tr>
            <td style="background-color: #0d0d0d; padding: 20px 30px; border-top: 1px solid #222;">
              <table width="100%" cellpadding="0" cellspacing="0">
                <tr>
                  <td style="color: #666; font-size: 13px;">
                    ${formattedDate}
                  </td>
                </tr>
                <tr>
                  <td style="color: #666; font-size: 13px; padding-top: 8px;">
                    Source: Landing Page Contact Form
                  </td>
                </tr>
              </table>
            </td>
          </tr>
        </table>
        
        <!-- Quick Actions -->
        <table width="600" cellpadding="0" cellspacing="0" style="margin-top: 20px;">
          <tr>
            <td align="center">
              <a href="mailto:${escapeHtml(email)}?subject=Re: CyberMesh Inquiry&body=Hi ${escapeHtml(name)},%0D%0A%0D%0AThank you for reaching out..." 
                 style="display: inline-block; background: linear-gradient(135deg, #ff4500 0%, #ff6b35 100%); color: #ffffff; padding: 12px 24px; border-radius: 6px; text-decoration: none; font-weight: 600; font-size: 14px;">
                Reply to ${escapeHtml(name)}
              </a>
            </td>
          </tr>
        </table>
      </td>
    </tr>
  </table>
</body>
</html>
  `.trim();
}

// Generate plain text version
function generatePlainTextTemplate(data: ContactEmailRequest): string {
  const { name, email, company, message } = data;
  const formattedDate = formatDate();
  
  return `
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
NEW CYBERMESH INQUIRY
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

From: ${name}
Email: ${email}${company ? `\nCompany: ${company}` : ""}

━━ MESSAGE ━━━━━━━━━━━━━━━━━━━━━━

${message}

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
${formattedDate}
Source: Landing Page Contact Form
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  `.trim();
}

// Send via Zoho ZeptoMail
async function sendViaZoho(
  to: string,
  subject: string,
  htmlContent: string,
  textContent: string,
  requestId: string
): Promise<{ success: boolean; messageId?: string; error?: string }> {
  const token = Deno.env.get("ZOHO_ZEPTOMAIL_TOKEN");
  const senderEmail = Deno.env.get("ZOHO_SENDER_EMAIL");
  const senderName = Deno.env.get("ZOHO_SENDER_NAME") || "CyberMesh";

  if (!token || !senderEmail) {
    return { success: false, error: "Email service not configured" };
  }

  logRequest(FUNCTION_NAME, requestId, "system", "ZOHO_SEND", { provider: "zoho" });

  try {
    const response = await fetch("https://api.zeptomail.com/v1.1/email", {
      method: "POST",
      headers: {
        "Accept": "application/json",
        "Content-Type": "application/json",
        "Authorization": token,
      },
      body: JSON.stringify({
        from: {
          address: senderEmail,
          name: senderName,
        },
        to: [{ email_address: { address: to } }],
        subject: subject,
        htmlbody: htmlContent,
        textbody: textContent,
      }),
    });

    const result = await response.json();

    if (!response.ok) {
      logResponse(FUNCTION_NAME, requestId, response.status, { provider: "zoho", error: "send_failed" });
      return { success: false, error: "Failed to send email" };
    }

    logResponse(FUNCTION_NAME, requestId, 200, { provider: "zoho", success: true });
    return { success: true, messageId: result.request_id };
  } catch (error: unknown) {
    logResponse(FUNCTION_NAME, requestId, 500, { 
      provider: "zoho", 
      error: error instanceof Error ? error.message : "Unknown" 
    });
    return { success: false, error: "Email service error" };
  }
}

// Generate JWT for Google Service Account
async function generateGoogleJWT(clientEmail: string, privateKey: string): Promise<string> {
  const header = { alg: "RS256", typ: "JWT" };
  const now = Math.floor(Date.now() / 1000);
  const payload = {
    iss: clientEmail,
    sub: clientEmail,
    scope: "https://www.googleapis.com/auth/gmail.send",
    aud: "https://oauth2.googleapis.com/token",
    iat: now,
    exp: now + 3600,
  };

  const encoder = new TextEncoder();
  const headerB64 = btoa(JSON.stringify(header)).replace(/=/g, "").replace(/\+/g, "-").replace(/\//g, "_");
  const payloadB64 = btoa(JSON.stringify(payload)).replace(/=/g, "").replace(/\+/g, "-").replace(/\//g, "_");
  const unsignedToken = `${headerB64}.${payloadB64}`;

  const pemContents = privateKey
    .replace(/-----BEGIN PRIVATE KEY-----/, "")
    .replace(/-----END PRIVATE KEY-----/, "")
    .replace(/\n/g, "");
  
  const binaryDer = Uint8Array.from(atob(pemContents), (c) => c.charCodeAt(0));
  
  const cryptoKey = await crypto.subtle.importKey(
    "pkcs8",
    binaryDer,
    { name: "RSASSA-PKCS1-v1_5", hash: "SHA-256" },
    false,
    ["sign"]
  );

  const signature = await crypto.subtle.sign(
    "RSASSA-PKCS1-v1_5",
    cryptoKey,
    encoder.encode(unsignedToken)
  );

  const signatureB64 = btoa(String.fromCharCode(...new Uint8Array(signature)))
    .replace(/=/g, "")
    .replace(/\+/g, "-")
    .replace(/\//g, "_");

  return `${unsignedToken}.${signatureB64}`;
}

// Send via Google Workspace Gmail API
async function sendViaGoogle(
  to: string,
  subject: string,
  htmlContent: string,
  fromEmail: string,
  requestId: string
): Promise<{ success: boolean; messageId?: string; error?: string }> {
  const clientEmail = Deno.env.get("GOOGLE_CLIENT_EMAIL");
  const privateKey = Deno.env.get("GOOGLE_PRIVATE_KEY");

  if (!clientEmail || !privateKey) {
    return { success: false, error: "Email service not configured" };
  }

  logRequest(FUNCTION_NAME, requestId, "system", "GOOGLE_SEND", { provider: "google" });

  try {
    const jwt = await generateGoogleJWT(clientEmail, privateKey);
    
    const tokenResponse = await fetch("https://oauth2.googleapis.com/token", {
      method: "POST",
      headers: { "Content-Type": "application/x-www-form-urlencoded" },
      body: new URLSearchParams({
        grant_type: "urn:ietf:params:oauth:grant-type:jwt-bearer",
        assertion: jwt,
      }),
    });

    const tokenData = await tokenResponse.json();
    
    if (!tokenResponse.ok) {
      logResponse(FUNCTION_NAME, requestId, tokenResponse.status, { provider: "google", error: "token_failed" });
      return { success: false, error: "Email service authentication failed" };
    }

    const emailLines = [
      `From: ${fromEmail}`,
      `To: ${to}`,
      `Subject: ${subject}`,
      "MIME-Version: 1.0",
      'Content-Type: text/html; charset="UTF-8"',
      "",
      htmlContent,
    ];
    
    const email = emailLines.join("\r\n");
    const encodedEmail = btoa(unescape(encodeURIComponent(email)))
      .replace(/\+/g, "-")
      .replace(/\//g, "_")
      .replace(/=+$/, "");

    const sendResponse = await fetch(
      `https://gmail.googleapis.com/gmail/v1/users/${fromEmail}/messages/send`,
      {
        method: "POST",
        headers: {
          "Authorization": `Bearer ${tokenData.access_token}`,
          "Content-Type": "application/json",
        },
        body: JSON.stringify({ raw: encodedEmail }),
      }
    );

    const sendResult = await sendResponse.json();

    if (!sendResponse.ok) {
      logResponse(FUNCTION_NAME, requestId, sendResponse.status, { provider: "google", error: "send_failed" });
      return { success: false, error: "Failed to send email" };
    }

    logResponse(FUNCTION_NAME, requestId, 200, { provider: "google", success: true });
    return { success: true, messageId: sendResult.id };
  } catch (error: unknown) {
    logResponse(FUNCTION_NAME, requestId, 500, { 
      provider: "google", 
      error: error instanceof Error ? error.message : "Unknown" 
    });
    return { success: false, error: "Email service error" };
  }
}

// Main handler
const handler = async (req: Request): Promise<Response> => {
  const requestId = generateRequestId();
  const origin = req.headers.get("origin");
  const clientId = getClientIdentifier(req);

  // Handle CORS preflight requests
  if (req.method === "OPTIONS") {
    return new Response(null, { 
      headers: { ...getCorsHeaders(origin), ...securityHeaders, "Access-Control-Allow-Methods": "POST, OPTIONS" } 
    });
  }

  // Log incoming request
  logRequest(FUNCTION_NAME, requestId, clientId, req.method);

  // Only allow POST requests
  if (req.method !== "POST") {
    logResponse(FUNCTION_NAME, requestId, 405, { reason: "method_not_allowed" });
    return createErrorResponse(origin, requestId, 405, "Method not allowed");
  }

  // Check Content-Type header
  const contentType = req.headers.get("content-type");
  if (!contentType || !contentType.includes("application/json")) {
    logResponse(FUNCTION_NAME, requestId, 415, { reason: "invalid_content_type" });
    return createErrorResponse(origin, requestId, 415, "Content-Type must be application/json");
  }

  // Check rate limit (stricter for contact form)
  const rateLimit = checkRateLimit(`${FUNCTION_NAME}:${clientId}`, CONTACT_RATE_LIMIT);
  if (!rateLimit.allowed) {
    logResponse(FUNCTION_NAME, requestId, 429, { reason: "rate_limit_exceeded" });
    return createRateLimitResponse(origin, requestId, rateLimit.resetInSec);
  }

  try {
    // Check request body size
    const contentLength = req.headers.get("content-length");
    if (contentLength && parseInt(contentLength, 10) > MAX_BODY_SIZE) {
      logResponse(FUNCTION_NAME, requestId, 413, { reason: "body_too_large" });
      return createErrorResponse(origin, requestId, 413, "Request body too large");
    }

    // Parse and validate request body
    let body: unknown;
    try {
      const text = await req.text();
      if (text.length > MAX_BODY_SIZE) {
        logResponse(FUNCTION_NAME, requestId, 413, { reason: "body_too_large" });
        return createErrorResponse(origin, requestId, 413, "Request body too large");
      }
      body = JSON.parse(text);
    } catch {
      logResponse(FUNCTION_NAME, requestId, 400, { reason: "invalid_json" });
      return createErrorResponse(origin, requestId, 400, "Invalid JSON in request body");
    }

    // Validate input
    const validation = validateInput(body);
    if (!validation.valid || !validation.sanitized) {
      logResponse(FUNCTION_NAME, requestId, 400, { reason: "validation_failed", detail: validation.error });
      return createErrorResponse(origin, requestId, 400, validation.error || "Validation failed");
    }

    const contactData = validation.sanitized;

    // Get configuration
    const emailProvider = Deno.env.get("EMAIL_PROVIDER") || "smtp";
    const adminEmail = Deno.env.get("ADMIN_NOTIFICATION_EMAIL");

    if (!adminEmail) {
      logResponse(FUNCTION_NAME, requestId, 500, { reason: "email_not_configured" });
      return createErrorResponse(origin, requestId, 500, "Email service not configured");
    }

    // Generate email content
    const subject = `New CyberMesh Inquiry: ${contactData.name}${contactData.company ? ` from ${contactData.company}` : ""}`;
    const htmlContent = generateEmailTemplate(contactData);
    const textContent = generatePlainTextTemplate(contactData);

    // Send email based on configured provider with failover
    let result: { success: boolean; messageId?: string; error?: string };
    let usedProvider = emailProvider;

    if (emailProvider === "smtp") {
      // SMTP primary (Google Workspace SMTP Relay)
      const smtpConfig = getSMTPConfig();
      
      if (!smtpConfig) {
        logResponse(FUNCTION_NAME, requestId, 500, { reason: "smtp_not_configured" });
        return createErrorResponse(origin, requestId, 500, "SMTP service not configured");
      }

      result = await sendViaSMTP(
        smtpConfig,
        { to: adminEmail, subject, htmlContent, textContent },
        requestId
      );

      // Fallback to Zoho if SMTP fails
      if (!result.success) {
        logRequest(FUNCTION_NAME, requestId, "system", "FAILOVER_TO_ZOHO", { reason: result.error });
        result = await sendViaZoho(adminEmail, subject, htmlContent, textContent, requestId);
        if (result.success) usedProvider = "zoho (failover)";
      }
    } else if (emailProvider === "google") {
      // Google API (legacy method)
      const senderEmail = Deno.env.get("GMAIL_SENDER_EMAIL") || adminEmail;
      result = await sendViaGoogle(adminEmail, subject, htmlContent, senderEmail, requestId);
      
      if (!result.success) {
        logRequest(FUNCTION_NAME, requestId, "system", "FAILOVER_TO_ZOHO", { reason: result.error });
        result = await sendViaZoho(adminEmail, subject, htmlContent, textContent, requestId);
        if (result.success) usedProvider = "zoho (failover)";
      }
    } else {
      // Zoho primary (default)
      result = await sendViaZoho(adminEmail, subject, htmlContent, textContent, requestId);
      
      if (!result.success) {
        // Try SMTP as fallback
        const smtpConfig = getSMTPConfig();
        if (smtpConfig) {
          logRequest(FUNCTION_NAME, requestId, "system", "FAILOVER_TO_SMTP", { reason: result.error });
          result = await sendViaSMTP(
            smtpConfig,
            { to: adminEmail, subject, htmlContent, textContent },
            requestId
          );
          if (result.success) usedProvider = "smtp (failover)";
        }
      }
    }

    if (!result.success) {
      logResponse(FUNCTION_NAME, requestId, 500, { reason: "email_send_failed", provider: usedProvider });
      return createErrorResponse(origin, requestId, 500, result.error || "Failed to send email");
    }

    // Log successful submission for backup purposes
    console.log(JSON.stringify({
      type: "CONTACT_SUBMISSION",
      requestId,
      timestamp: new Date().toISOString(),
      provider: usedProvider,
      from: { name: contactData.name, email: contactData.email, company: contactData.company },
      messagePreview: contactData.message.substring(0, 100) + (contactData.message.length > 100 ? "..." : ""),
    }));

    logResponse(FUNCTION_NAME, requestId, 200, { success: true });

    return new Response(
      JSON.stringify({ 
        success: true, 
        message: "Your message has been sent successfully",
        requestId,
      }),
      { 
        status: 200, 
        headers: getResponseHeaders(origin, requestId, {
          remaining: rateLimit.remaining,
          resetInSec: rateLimit.resetInSec,
        }) 
      }
    );
  } catch (error: unknown) {
    logResponse(FUNCTION_NAME, requestId, 500, { 
      reason: "unexpected_error",
      error: error instanceof Error ? error.message : "Unknown" 
    });
    return createErrorResponse(origin, requestId, 500, "An unexpected error occurred");
  }
};

serve(handler);
