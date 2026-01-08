// SMTP Email sending utility for contact form
// Uses Google Workspace SMTP Relay (smtp-relay.gmail.com)

interface SMTPConfig {
  host: string;
  port: number;
  user: string;
  password: string;
  fromEmail: string;
  fromName: string;
}

interface EmailData {
  to: string;
  subject: string;
  htmlContent: string;
  textContent: string;
}

/**
 * Send email via SMTP using smtp-relay.gmail.com
 */
export async function sendViaSMTP(
  config: SMTPConfig,
  emailData: EmailData,
  requestId: string
): Promise<{ success: boolean; messageId?: string; error?: string }> {
  const { host, port, user, password, fromEmail, fromName } = config;
  const { to, subject, htmlContent, textContent } = emailData;

  try {
    // Create connection to SMTP server
    const conn = await Deno.connect({
      hostname: host,
      port: port,
      transport: "tcp",
    });

    const encoder = new TextEncoder();
    const decoder = new TextDecoder();

    // Helper to read SMTP response
    const readResponse = async (): Promise<string> => {
      const buffer = new Uint8Array(1024);
      const n = await conn.read(buffer);
      if (n === null) throw new Error("Connection closed");
      return decoder.decode(buffer.subarray(0, n));
    };

    // Helper to send SMTP command
    const sendCommand = async (command: string): Promise<string> => {
      await conn.write(encoder.encode(command + "\r\n"));
      return await readResponse();
    };

    // 1. Read greeting
    const greeting = await readResponse();
    if (!greeting.startsWith("220")) {
      throw new Error(`SMTP connection failed: ${greeting}`);
    }

    // 2. EHLO
    const ehloResponse = await sendCommand(`EHLO ${host}`);
    if (!ehloResponse.startsWith("250")) {
      throw new Error(`EHLO failed: ${ehloResponse}`);
    }

    // 3. STARTTLS
    const tlsResponse = await sendCommand("STARTTLS");
    if (!tlsResponse.startsWith("220")) {
      throw new Error(`STARTTLS failed: ${tlsResponse}`);
    }

    // 4. Upgrade to TLS
    const tlsConn = await Deno.startTls(conn, { hostname: host });

    const tlsSendCommand = async (command: string): Promise<string> => {
      await tlsConn.write(encoder.encode(command + "\r\n"));
      const buffer = new Uint8Array(1024);
      const n = await tlsConn.read(buffer);
      if (n === null) throw new Error("Connection closed");
      return decoder.decode(buffer.subarray(0, n));
    };

    // 5. EHLO again after TLS
    const ehloTlsResponse = await tlsSendCommand(`EHLO ${host}`);
    if (!ehloTlsResponse.startsWith("250")) {
      throw new Error(`EHLO (TLS) failed: ${ehloTlsResponse}`);
    }

    // 6. AUTH LOGIN
    const authResponse = await tlsSendCommand("AUTH LOGIN");
    if (!authResponse.startsWith("334")) {
      throw new Error(`AUTH LOGIN failed: ${authResponse}`);
    }

    // 7. Send username (base64 encoded)
    const userB64 = btoa(user);
    const userResponse = await tlsSendCommand(userB64);
    if (!userResponse.startsWith("334")) {
      throw new Error(`Username authentication failed: ${userResponse}`);
    }

    // 8. Send password (base64 encoded)
    const passB64 = btoa(password);
    const passResponse = await tlsSendCommand(passB64);
    if (!passResponse.startsWith("235")) {
      throw new Error(`Password authentication failed: ${passResponse}`);
    }

    // 9. MAIL FROM
    const mailFromResponse = await tlsSendCommand(`MAIL FROM:<${fromEmail}>`);
    if (!mailFromResponse.startsWith("250")) {
      throw new Error(`MAIL FROM failed: ${mailFromResponse}`);
    }

    // 10. RCPT TO
    const rcptToResponse = await tlsSendCommand(`RCPT TO:<${to}>`);
    if (!rcptToResponse.startsWith("250")) {
      throw new Error(`RCPT TO failed: ${rcptToResponse}`);
    }

    // 11. DATA
    const dataResponse = await tlsSendCommand("DATA");
    if (!dataResponse.startsWith("354")) {
      throw new Error(`DATA failed: ${dataResponse}`);
    }

    // 12. Send email headers and body
    const emailContent = [
      `From: ${fromName} <${fromEmail}>`,
      `To: ${to}`,
      `Subject: ${subject}`,
      `MIME-Version: 1.0`,
      `Content-Type: multipart/alternative; boundary="boundary123"`,
      `X-Request-ID: ${requestId}`,
      "",
      "--boundary123",
      "Content-Type: text/plain; charset=UTF-8",
      "",
      textContent,
      "",
      "--boundary123",
      "Content-Type: text/html; charset=UTF-8",
      "",
      htmlContent,
      "",
      "--boundary123--",
      ".",
    ].join("\r\n");

    const sendResponse = await tlsSendCommand(emailContent);
    if (!sendResponse.startsWith("250")) {
      throw new Error(`Email send failed: ${sendResponse}`);
    }

    // 13. QUIT
    await tlsSendCommand("QUIT");
    tlsConn.close();

    console.log(`[SMTP] Email sent successfully via ${host} (Request ID: ${requestId})`);

    return {
      success: true,
      messageId: requestId,
    };
  } catch (error: unknown) {
    console.error(`[SMTP] Error sending email:`, error);
    return {
      success: false,
      error: error instanceof Error ? error.message : "Unknown SMTP error",
    };
  }
}

/**
 * Get SMTP configuration from environment variables
 */
export function getSMTPConfig(): SMTPConfig | null {
  const host = Deno.env.get("SMTP_HOST");
  const portStr = Deno.env.get("SMTP_PORT");
  const user = Deno.env.get("SMTP_USER");
  const password = Deno.env.get("SMTP_PASSWORD");
  const fromEmail = Deno.env.get("SMTP_FROM_EMAIL");
  const fromName = Deno.env.get("SMTP_FROM_NAME") || "CyberMesh";

  if (!host || !portStr || !user || !password || !fromEmail) {
    console.error("[SMTP] Missing required environment variables");
    return null;
  }

  const port = parseInt(portStr, 10);
  if (isNaN(port)) {
    console.error("[SMTP] Invalid SMTP_PORT value");
    return null;
  }

  return {
    host,
    port,
    user,
    password,
    fromEmail,
    fromName,
  };
}
