import { type FormEvent, useMemo, useRef, useState } from "react";
import { Paperclip, X } from "lucide-react";
import { post, postForm } from "../../lib/api";
import type { MessageDetail } from "../../lib/types";
import { addressLine, formatBytes } from "../../lib/format";
import { forwardSeed, replySeed } from "../../lib/mail-html";
import { Button, ConfirmDialog, ErrorNote, IconButton, Input, toast } from "../../components/ui";
import { RichEditor } from "../../components/RichEditor";

export type ComposeSeed =
  | { mode: "new" }
  | { mode: "reply" | "reply_all" | "forward"; original: MessageDetail };

export default function Compose({
  seed,
  me,
  onClose,
  onSent,
}: {
  seed: ComposeSeed;
  me: string;
  onClose: () => void;
  onSent: () => void;
}) {
  const initial = useMemo(() => seedValues(seed), [seed]);
  const [to, setTo] = useState(initial.to);
  const [cc, setCc] = useState(initial.cc);
  const [bcc, setBcc] = useState("");
  const [showCc, setShowCc] = useState(initial.cc !== "");
  const [subject, setSubject] = useState(initial.subject);
  // The rich editor manages HTML; we keep the latest HTML and a derived
  // plain-text version for the multipart/alternative message.
  const [bodyHtml, setBodyHtml] = useState(initial.html);
  const [bodyText, setBodyText] = useState(initial.text);
  const [bodyTouched, setBodyTouched] = useState(false);
  const [files, setFiles] = useState<File[]>([]);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [confirmDiscard, setConfirmDiscard] = useState(false);
  const fileInput = useRef<HTMLInputElement>(null);

  const original = seed.mode === "new" ? null : seed.original;
  const isReply = seed.mode === "reply" || seed.mode === "reply_all";
  const dirty =
    to !== initial.to ||
    cc !== initial.cc ||
    bcc !== "" ||
    subject !== initial.subject ||
    bodyTouched ||
    files.length > 0;

  function requestClose() {
    if (busy) return;
    if (!dirty) {
      onClose();
      return;
    }
    setConfirmDiscard(true);
  }

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      if (isReply) {
        if (!original) throw new Error("Original message missing");
        await post("/api/mail/reply", {
          id: original.id,
          body: bodyText,
          html_body: bodyHtml,
          reply_all: seed.mode === "reply_all",
        });
      } else if (seed.mode === "forward") {
        if (!original) throw new Error("Original message missing");
        // The client builds the quoted original (it has both text and HTML);
        // the server re-attaches the original's attachments.
        await post("/api/mail/forward", {
          id: original.id,
          to,
          cc,
          bcc,
          subject,
          body: bodyText,
          html_body: bodyHtml,
        });
      } else {
        const form = new FormData();
        form.set("to", to);
        form.set("cc", cc);
        form.set("bcc", bcc);
        form.set("subject", subject);
        form.set("body", bodyText);
        form.set("html_body", bodyHtml);
        for (const f of files) form.append("attachments", f, f.name);
        await postForm("/api/mail/send", form);
      }
      toast("Message sent");
      onSent();
    } catch (err: any) {
      setError(err.message);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="fixed inset-0 z-40 flex items-end justify-center bg-black/40 p-0 backdrop-blur-[1px] md:items-center md:p-6">
      <form
        onSubmit={submit}
        className="animate-rise flex max-h-[92vh] w-full max-w-2xl flex-col rounded-t-xl border border-line bg-overlay shadow-pop md:rounded-xl"
      >
        <header className="flex items-center justify-between border-b border-line px-5 py-3">
          <h2 className="text-[14px] font-semibold text-ink">
            {seed.mode === "new" && "New message"}
            {seed.mode === "reply" && "Reply"}
            {seed.mode === "reply_all" && "Reply all"}
            {seed.mode === "forward" && "Forward"}
          </h2>
          <IconButton
            type="button"
            title="Close composer"
            onClick={requestClose}
            icon={<X size={15} />}
          />
        </header>

        <div className="flex-1 space-y-3 overflow-y-auto px-5 py-4">
          {!isReply && (
            <div className="flex items-center gap-2">
              <Input
                type="text"
                placeholder="To — separate addresses with commas"
                required
                value={to}
                onChange={(e) => setTo(e.target.value)}
                autoFocus={seed.mode !== "reply"}
              />
              {!showCc && (
                <button
                  type="button"
                  className="shrink-0 text-[12px] text-faint hover:text-ink"
                  onClick={() => setShowCc(true)}
                >
                  Cc/Bcc
                </button>
              )}
            </div>
          )}
          {isReply && original && (
            <div className="rounded-lg bg-inset px-3 py-2 text-[12.5px] text-muted">
              to {addressLine(original.reply_to?.length ? original.reply_to : original.header.from)}
              {seed.mode === "reply_all" && " and everyone on the thread"}
            </div>
          )}
          {showCc && !isReply && (
            <>
              <Input
                placeholder="Cc"
                value={cc}
                onChange={(e) => setCc(e.target.value)}
              />
              <Input
                placeholder="Bcc"
                value={bcc}
                onChange={(e) => setBcc(e.target.value)}
              />
            </>
          )}
          {!isReply && (
            <Input
              placeholder="Subject"
              value={subject}
              onChange={(e) => setSubject(e.target.value)}
            />
          )}
          <RichEditor
            initialHTML={initial.html}
            placeholder="Write something…"
            autoFocus={isReply}
            onChange={(html, text) => {
              setBodyTouched(true);
              setBodyHtml(html);
              setBodyText(text);
            }}
          />
          {files.length > 0 && (
            <div className="flex flex-wrap gap-2">
              {files.map((f, i) => (
                <span
                  key={`${f.name}-${i}`}
                  className="flex items-center gap-2 rounded-lg border border-line bg-inset px-2.5 py-1.5 text-[12px] text-muted"
                >
                  <Paperclip size={12} />
                  {f.name} · {formatBytes(f.size)}
                  <button
                    type="button"
                    onClick={() => setFiles((prev) => prev.filter((_, j) => j !== i))}
                    className="text-faint hover:text-danger"
                  >
                    <X size={12} />
                  </button>
                </span>
              ))}
            </div>
          )}
          <ErrorNote>{error}</ErrorNote>
        </div>

        <footer className="flex items-center justify-between border-t border-line px-5 py-3">
          <div className="flex items-center gap-2">
            <input
              ref={fileInput}
              type="file"
              multiple
              hidden
              onChange={(e) => {
                const chosen = Array.from(e.target.files ?? []);
                setFiles((prev) => [...prev, ...chosen]);
                e.target.value = "";
              }}
            />
            {seed.mode === "new" && (
              <Button type="button" variant="ghost" size="sm" onClick={() => fileInput.current?.click()}>
                <Paperclip size={13} />
                Attach
              </Button>
            )}
            {seed.mode === "forward" && (
              <span className="text-[11.5px] text-faint">original attachments included</span>
            )}
            <span className="text-[11.5px] text-faint">sending as {me}</span>
          </div>
          <div className="flex items-center gap-2">
            <Button type="button" variant="ghost" onClick={requestClose} disabled={busy}>
              Discard
            </Button>
            <Button type="submit" variant="primary" busy={busy}>
              Send
            </Button>
          </div>
        </footer>
      </form>
      {confirmDiscard && (
        <ConfirmDialog
          title="Discard draft?"
          confirmLabel="Discard draft"
          danger
          closeAfterConfirm={false}
          onClose={() => setConfirmDiscard(false)}
          onConfirm={() => {
            setConfirmDiscard(false);
            onClose();
          }}
        />
      )}
    </div>
  );
}

function seedValues(seed: ComposeSeed): {
  to: string;
  cc: string;
  subject: string;
  html: string;
  text: string;
} {
  if (seed.mode === "new") {
    return { to: "", cc: "", subject: "", html: "", text: "" };
  }
  const o = seed.original;
  if (seed.mode === "forward") {
    const { html, text } = forwardSeed(o);
    return {
      to: "",
      cc: "",
      subject: o.header.subject.toLowerCase().startsWith("fwd:")
        ? o.header.subject
        : `Fwd: ${o.header.subject}`,
      html,
      text,
    };
  }
  // reply / reply_all — the To/Cc/Subject inputs are never shown for replies,
  // and the server derives recipients and subject from the original message id,
  // so the client sends none of them.
  const { html, text } = replySeed(o);
  return { to: "", cc: "", subject: "", html, text };
}
