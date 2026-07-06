// Helpers for building the quoted-original HTML the rich editor is seeded
// with on reply and forward. Both an HTML and a plain-text version are
// produced so the outgoing message can be multipart/alternative.
//
// Colors here are intentionally inline hex, NOT theme tokens (var(--muted),
// CSS classes): this HTML is sent as the email body, and recipients' mail
// clients (Gmail, Outlook, …) have none of wispbox's stylesheet, so anything
// but self-contained inline styles would render unstyled for them.
import type { MessageDetail } from "./types";
import { addressLine } from "./format";

function escapeHtml(s: string): string {
  return s
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
}

// originalAsHtml returns the original message body as HTML: its sanitized
// HTML body if present, otherwise its plain text converted to HTML.
function originalAsHtml(o: MessageDetail): string {
  if (o.html_body) return o.html_body;
  return escapeHtml(o.text_body || "").replace(/\n/g, "<br>");
}

function originalAsText(o: MessageDetail): string {
  if (o.text_body) return o.text_body;
  // crude fallback if only HTML exists
  const d = document.createElement("div");
  d.innerHTML = o.html_body || "";
  return d.textContent || "";
}

// replySeed builds the editor's initial HTML and the plain-text fallback for
// a reply: an empty line to type into, above a quoted block of the original.
export function replySeed(o: MessageDetail): { html: string; text: string } {
  const when = new Date(o.header.date).toLocaleString();
  const who = addressLine(o.header.from);
  const attribution = `On ${when}, ${who} wrote:`;
  const html =
    `<p><br></p>` +
    `<p style="color:#6b7280">${escapeHtml(attribution)}</p>` +
    `<blockquote style="margin:0 0 0 8px;padding-left:12px;border-left:2px solid #d1d5db;color:#6b7280">` +
    `${originalAsHtml(o)}</blockquote>`;
  const text =
    `\n\n${attribution}\n` +
    originalAsText(o)
      .split("\n")
      .map((l) => `> ${l}`)
      .join("\n");
  return { html, text };
}

// forwardSeed builds the editor's initial HTML and plain-text fallback for a
// forward: a forwarded-message header block followed by the original.
export function forwardSeed(o: MessageDetail): { html: string; text: string } {
  const from = addressLine(o.header.from);
  const to = addressLine(o.header.to);
  const date = new Date(o.header.date).toLocaleString();
  const subject = o.header.subject;

  const headerHtml =
    `<p><br></p>` +
    `<div style="color:#6b7280">---------- Forwarded message ----------<br>` +
    `From: ${escapeHtml(from)}<br>` +
    `Date: ${escapeHtml(date)}<br>` +
    `Subject: ${escapeHtml(subject)}<br>` +
    (to ? `To: ${escapeHtml(to)}<br>` : "") +
    `</div>`;
  const html = headerHtml + `<div>${originalAsHtml(o)}</div>`;

  const text =
    `\n\n---------- Forwarded message ----------\n` +
    `From: ${from}\nDate: ${date}\nSubject: ${subject}\n` +
    (to ? `To: ${to}\n` : "") +
    `\n` +
    originalAsText(o);
  return { html, text };
}
