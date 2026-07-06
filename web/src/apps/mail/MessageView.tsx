import { useEffect, useRef, useState } from "react";
import {
  ArrowLeft,
  Download,
  FolderInput,
  Forward,
  ImageOff,
  MailOpen,
  Paperclip,
  Reply,
  ReplyAll,
  Trash2,
} from "lucide-react";
import { get, post } from "../../lib/api";
import type { Folder, MessageDetail } from "../../lib/types";
import { addressLine, formatBytes, formatDate, senderLabel } from "../../lib/format";
import { Button, EmptyState, ErrorNote, IconButton, Spinner, toast } from "../../components/ui";
import type { ComposeSeed } from "./Compose";

export default function MessageView({
  id,
  folders,
  onClose,
  onGone,
  onCompose,
}: {
  id: string | null;
  folders: Folder[];
  onClose: () => void;
  onGone: () => void;
  onCompose: (seed: ComposeSeed) => void;
}) {
  const [msg, setMsg] = useState<MessageDetail | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [showRemote, setShowRemote] = useState(false);
  const [moveOpen, setMoveOpen] = useState(false);

  useEffect(() => {
    setMsg(null);
    setError("");
    setShowRemote(false);
    setMoveOpen(false);
    if (!id) return;
    let cancelled = false;
    setBusy(true);
    get<MessageDetail>(`/api/mail/messages/${id}`)
      .then(async (m) => {
        if (cancelled) return;
        setMsg(m);
        if (!m.header.seen) {
          post(`/api/mail/messages/${id}/mark-read`, { read: true }).catch(() => {});
        }
      })
      .catch((e) => !cancelled && setError(e.message))
      .finally(() => !cancelled && setBusy(false));
    return () => {
      cancelled = true;
    };
  }, [id]);

  async function reloadWithRemote() {
    if (!id) return;
    try {
      const m = await get<MessageDetail>(`/api/mail/messages/${id}?remote=1`);
      setMsg(m);
      setShowRemote(true);
    } catch (e: any) {
      toast(e.message, "error");
    }
  }

  async function act(action: () => Promise<unknown>, doneMsg: string) {
    try {
      await action();
      toast(doneMsg);
      onGone();
    } catch (e: any) {
      toast(e.message, "error");
    }
  }

  if (!id) {
    return (
      <main className="hidden min-w-0 flex-1 items-center justify-center md:flex">
        <EmptyState title="Select a message" />
      </main>
    );
  }

  return (
    <main className="flex min-w-0 flex-1 flex-col bg-bg">
      <header className="flex items-center gap-1 border-b border-line px-3 py-2">
        <IconButton
          onClick={onClose}
          title="Back"
          size="md"
          icon={<ArrowLeft size={15} />}
        />
        <div className="flex-1" />
        {msg && (
          <>
            <IconAction
              title="Reply"
              emphasis
              onClick={() => onCompose({ mode: "reply", original: msg })}
            >
              <Reply size={16} />
            </IconAction>
            <IconAction
              title="Reply all"
              emphasis
              onClick={() => onCompose({ mode: "reply_all", original: msg })}
            >
              <ReplyAll size={16} />
            </IconAction>
            <IconAction
              title="Forward"
              emphasis
              onClick={() => onCompose({ mode: "forward", original: msg })}
            >
              <Forward size={16} />
            </IconAction>
            <span className="mx-1 h-5 w-px bg-line" />
            <IconAction
              title="Mark unread"
              onClick={() =>
                act(
                  () => post(`/api/mail/messages/${id}/mark-read`, { read: false }),
                  "Marked unread",
                )
              }
            >
              <MailOpen size={15} />
            </IconAction>
            <div className="relative">
              <IconAction title="Move to folder" onClick={() => setMoveOpen((v) => !v)}>
                <FolderInput size={15} />
              </IconAction>
              {moveOpen && (
                <>
                  {/* Transparent backdrop: any outside click dismisses the menu. */}
                  <button
                    type="button"
                    aria-hidden
                    tabIndex={-1}
                    className="fixed inset-0 z-10 cursor-default"
                    onClick={() => setMoveOpen(false)}
                  />
                  <div className="absolute right-0 top-9 z-20 w-44 animate-rise rounded-lg border border-line bg-overlay p-1 shadow-pop">
                    {folders
                      .filter((f) => f.name !== msg.header.folder)
                      .map((f) => (
                      <button
                        key={f.name}
                        className="block w-full rounded-md px-2.5 py-1.5 text-left text-[13px] text-muted hover:bg-inset hover:text-ink"
                        onClick={() =>
                          act(
                            () => post(`/api/mail/messages/${id}/move`, { folder: f.name }),
                            `Moved to ${f.name}`,
                          )
                        }
                      >
                        {f.name}
                      </button>
                    ))}
                  </div>
                </>
              )}
            </div>
            <IconAction
              title={msg.header.folder === "Trash" ? "Delete forever" : "Move to trash"}
              danger
              onClick={() =>
                act(
                  () => post(`/api/mail/messages/${id}/delete`, {}),
                  msg.header.folder === "Trash" ? "Deleted forever" : "Moved to trash",
                )
              }
            >
              <Trash2 size={15} />
            </IconAction>
          </>
        )}
      </header>

      <div className="flex-1 overflow-y-auto">
        {busy && <Spinner />}
        {error && (
          <div className="p-4">
            <ErrorNote>{error}</ErrorNote>
          </div>
        )}
        {msg && (
          <article className="mx-auto max-w-[720px] px-5 py-6 md:px-8">
            <h1 className="text-[19px] font-semibold leading-snug text-ink">
              {msg.header.subject || "(no subject)"}
            </h1>
            <div className="mt-4 flex items-start gap-3 border-b border-line pb-4">
              <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-full bg-accent-dim text-[13px] font-semibold uppercase text-accent">
                {senderLabel(msg.header.from).slice(0, 1)}
              </div>
              <div className="min-w-0 flex-1 text-[12.5px]">
                <div className="flex items-baseline justify-between gap-3">
                  <span className="truncate font-medium text-ink">
                    {addressLine(msg.header.from)}
                  </span>
                  <span className="shrink-0 text-faint">{formatDate(msg.header.date)}</span>
                </div>
                <div className="mt-0.5 truncate text-muted">to {addressLine(msg.header.to)}</div>
                {msg.cc && msg.cc.length > 0 && (
                  <div className="truncate text-muted">cc {addressLine(msg.cc)}</div>
                )}
              </div>
            </div>

            {msg.has_blocked_images && !showRemote && (
              <button
                onClick={reloadWithRemote}
                className="mt-4 flex w-full items-center gap-2 rounded-lg border border-line bg-inset px-3 py-2 text-[12.5px] text-muted transition-colors hover:border-accent/40 hover:text-ink"
              >
                <ImageOff size={14} />
                Load remote images
              </button>
            )}

            <div className="pt-5">
              {msg.html_body ? (
                <HtmlBody html={msg.html_body} />
              ) : (
                <div className="email-text text-ink/90">{msg.text_body || "(empty message)"}</div>
              )}
            </div>

            {msg.attachments.length > 0 && (
              <div className="mt-8 border-t border-line pt-4">
                <div className="mb-2 flex items-center gap-1.5 text-[12px] font-medium uppercase text-faint">
                  <Paperclip size={12} />
                  {msg.attachments.length} attachment{msg.attachments.length > 1 ? "s" : ""}
                </div>
                <div className="flex flex-wrap gap-2">
                  {msg.attachments.map((a) => (
                    <a
                      key={a.id}
                      href={`/api/mail/attachments/${a.id}`}
                      download={a.filename}
                      className="group flex items-center gap-2.5 rounded-lg border border-line bg-inset px-3 py-2 transition-colors hover:border-accent/40"
                    >
                      <Download size={14} className="text-faint group-hover:text-accent" />
                      <div>
                        <div className="text-[13px] font-medium text-ink">{a.filename}</div>
                        <div className="text-[11px] text-faint">
                          {a.mime_type} · {formatBytes(a.size)}
                        </div>
                      </div>
                    </a>
                  ))}
                </div>
              </div>
            )}
          </article>
        )}
      </div>
    </main>
  );
}

function IconAction({
  title,
  onClick,
  danger,
  emphasis,
  children,
}: {
  title: string;
  onClick: () => void;
  danger?: boolean;
  emphasis?: boolean;
  children: React.ReactNode;
}) {
  return (
    <IconButton
      title={title}
      aria-label={title}
      onClick={onClick}
      tone={danger ? "danger" : "default"}
      size="md"
      className={emphasis ? "!text-muted hover:!text-ink" : ""}
      icon={children}
    />
  );
}

/**
 * Sanitized HTML rendered inside a sandboxed iframe: even though the server
 * strips scripts, the sandbox (no allow-scripts) plus the page CSP make
 * execution impossible by construction. Height auto-syncs to content.
 */
function HtmlBody({ html }: { html: string }) {
  const ref = useRef<HTMLIFrameElement>(null);
  const [height, setHeight] = useState(200);

  const doc = `<!doctype html><html><head><meta charset="utf-8"><style>
    :root { color-scheme: light dark; }
    body { margin: 0; font-family: "Instrument Sans Variable", ui-sans-serif, system-ui, sans-serif;
           font-size: 14.5px; line-height: 1.6; color: ${getComputedStyle(document.documentElement).getPropertyValue("--text") || "#e6eaf2"};
           overflow-wrap: anywhere; }
    a { color: ${getComputedStyle(document.documentElement).getPropertyValue("--accent") || "#6ee7b7"}; }
    img { max-width: 100%; height: auto; }
    blockquote { border-left: 3px solid rgba(128,128,128,.35); margin: 0.5em 0; padding-left: 12px; }
    pre { white-space: pre-wrap; }
  </style></head><body>${html}</body></html>`;

  useEffect(() => {
    const iframe = ref.current;
    if (!iframe) return;
    const sync = () => {
      const h = iframe.contentDocument?.documentElement?.scrollHeight;
      if (h && h > 0) setHeight(Math.min(h + 8, 20000));
    };
    iframe.addEventListener("load", sync);
    const t = setInterval(sync, 400);
    setTimeout(() => clearInterval(t), 3000);
    return () => {
      iframe.removeEventListener("load", sync);
      clearInterval(t);
    };
  }, [doc]);

  return (
    <iframe
      ref={ref}
      title="message"
      // allow-same-origin (but NOT allow-scripts) lets embedded cid: images
      // load with the session cookie and lets us read scrollHeight for the
      // height sync. Scripts cannot run — the server also strips them.
      sandbox="allow-same-origin"
      srcDoc={doc}
      style={{ height }}
      className="w-full border-0 bg-transparent"
    />
  );
}
