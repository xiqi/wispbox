import { Menu, Paperclip, RefreshCw, Search } from "lucide-react";
import type { MessageHeader } from "../../lib/types";
import { formatWhen, senderLabel } from "../../lib/format";
import { Button, EmptyState, ErrorNote, IconButton, Spinner } from "../../components/ui";

export default function MessageList({
  folder,
  messages,
  total,
  busy,
  error,
  query,
  onQuery,
  selectedId,
  onFolders,
  onSelect,
  nextCursor,
  onLoadMore,
  onRefresh,
  refreshing,
  hidden,
}: {
  folder: string;
  messages: MessageHeader[];
  total: number;
  busy: boolean;
  error: string;
  query: string;
  onQuery: (q: string) => void;
  selectedId: string | null;
  onFolders: () => void;
  onSelect: (m: MessageHeader) => void;
  nextCursor?: string;
  onLoadMore: () => void;
  onRefresh: () => void;
  refreshing: boolean;
  hidden: boolean;
}) {
  return (
    <div
      className={`${hidden ? "hidden" : "flex"} min-w-0 flex-1 flex-col border-r border-line md:flex md:max-w-[400px] md:flex-none md:basis-[380px]`}
    >
      <header className="flex items-center gap-2 border-b border-line px-3 py-2.5">
        <IconButton
          onClick={onFolders}
          title="Folders"
          size="md"
          className="md:hidden"
          icon={<Menu size={15} />}
        />
        <div className="relative flex-1">
          <Search size={14} className="absolute left-2.5 top-1/2 -translate-y-1/2 text-faint" />
          <input
            value={query}
            onChange={(e) => onQuery(e.target.value)}
            placeholder={`Search ${folder === "INBOX" ? "inbox" : folder}…`}
            className="h-8 w-full rounded-lg border border-transparent bg-inset pl-8 pr-3 text-[13px] leading-none text-ink placeholder:text-faint transition-shadow focus:border-accent/40 focus:outline-none focus:ring-2 focus:ring-accent/20"
          />
        </div>
        <IconButton
          aria-label="Refresh messages"
          disabled={busy || refreshing}
          onClick={onRefresh}
          title="Refresh messages"
          size="md"
          className="active:scale-95"
          icon={<RefreshCw size={14} className={busy || refreshing ? "animate-spin" : ""} />}
        />
      </header>

      <div className="flex-1 overflow-y-auto">
        {error && (
          <div className="p-3">
            <ErrorNote>{error}</ErrorNote>
          </div>
        )}
        {busy && messages.length === 0 && <Spinner />}
        {!busy && messages.length === 0 && !error && (
          <EmptyState title={query ? "No messages match" : "Nothing here"} />
        )}
        <ul>
          {messages.map((m) => {
            const active = m.id === selectedId;
            return (
              <li key={m.id}>
                <button
                  onClick={() => onSelect(m)}
                  className={`group relative block w-full border-b border-line px-4 py-3 text-left transition-colors ${
                    active ? "bg-accent-dim" : "hover:bg-inset"
                  }`}
                >
                  {!m.seen && (
                    <span className="absolute left-1.5 top-1/2 h-1.5 w-1.5 -translate-y-1/2 rounded-full bg-accent shadow-[0_0_8px_var(--glow)]" />
                  )}
                  <div className="flex items-baseline justify-between gap-2">
                    <span
                      className={`truncate text-[13.5px] ${m.seen ? "text-muted" : "font-semibold text-ink"}`}
                    >
                      {senderLabel(m.from)}
                    </span>
                    <span className="shrink-0 text-[11.5px] tabular-nums text-faint">
                      {formatWhen(m.date)}
                    </span>
                  </div>
                  <div className="mt-0.5 flex items-center gap-1.5">
                    <span
                      className={`truncate text-[13px] ${m.seen ? "text-faint" : "text-ink/90"}`}
                    >
                      {m.subject || "(no subject)"}
                    </span>
                    {m.has_attachments && (
                      <Paperclip size={12} className="shrink-0 text-faint" />
                    )}
                  </div>
                </button>
              </li>
            );
          })}
        </ul>
        {nextCursor && (
          <div className="flex justify-center p-3">
            <Button size="sm" onClick={onLoadMore} busy={busy}>
              Load more ({messages.length} of {total})
            </Button>
          </div>
        )}
      </div>
    </div>
  );
}
