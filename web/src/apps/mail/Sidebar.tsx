import {
  Archive,
  FileText,
  Inbox,
  LogOut,
  PenLine,
  Send,
  Settings2,
  ShieldAlert,
  Trash2,
} from "lucide-react";
import { useState } from "react";
import { clearCsrf, post } from "../../lib/api";
import type { Folder } from "../../lib/types";
import { Button, IconButton, Modal, ThemeToggle, Wordmark } from "../../components/ui";
import AccountSecurity from "../../components/AccountSecurity";

const roleIcons: Record<string, typeof Inbox> = {
  inbox: Inbox,
  sent: Send,
  drafts: FileText,
  trash: Trash2,
  junk: ShieldAlert,
  custom: Archive,
};

function emailParts(email: string) {
  const at = email.indexOf("@");
  if (at <= 0) return { local: email, domain: "" };
  return { local: email.slice(0, at), domain: email.slice(at + 1) };
}

export default function Sidebar({
  me,
  folders,
  active,
  onSelect,
  onCompose,
  onLoggedOut,
  hidden,
}: {
  me: string;
  folders: Folder[];
  active: string;
  onSelect: (folder: string) => void;
  onCompose: () => void;
  onLoggedOut: () => void;
  hidden: boolean;
}) {
  const [securityOpen, setSecurityOpen] = useState(false);
  const account = emailParts(me);
  return (
    <aside
      className={`${hidden ? "hidden" : "flex"} w-full shrink-0 flex-col border-r border-line bg-bg-deep md:flex md:w-[218px]`}
    >
      <div className="flex items-center justify-between px-4 pb-2 pt-4">
        <Wordmark />
        <ThemeToggle />
      </div>

      <div className="px-3 pb-2 pt-3">
        <Button
          type="button"
          variant="primary"
          onClick={onCompose}
          className="w-full"
        >
          <PenLine size={14} />
          Compose
        </Button>
      </div>

      <nav className="flex-1 space-y-0.5 overflow-y-auto px-3 py-2">
        {folders.map((f) => {
          const Icon = roleIcons[f.role] ?? Archive;
          const isActive = f.name === active;
          return (
            <button
              key={f.name}
              onClick={() => onSelect(f.name)}
              className={`flex w-full items-center gap-2.5 rounded-lg px-2.5 py-[7px] text-[13.5px] transition-colors ${
                isActive
                  ? "bg-accent-dim font-medium text-accent"
                  : "text-muted hover:bg-inset hover:text-ink"
              }`}
            >
              <Icon size={15} strokeWidth={isActive ? 2.2 : 1.8} />
              <span className="flex-1 truncate text-left">
                {f.role === "inbox" ? "Inbox" : f.name}
              </span>
              {f.unseen > 0 && (
                <span
                  className={`rounded-full px-1.5 text-[11px] font-semibold tabular-nums ${
                    isActive ? "text-accent" : "text-muted"
                  }`}
                >
                  {f.unseen}
                </span>
              )}
            </button>
          );
        })}
      </nav>

      <footer className="border-t border-line px-4 py-3">
        <div className="space-y-2">
          <div className="min-w-0 text-[12.5px] font-medium leading-tight">
            <div className="break-words text-ink">{account.local}</div>
            {account.domain && <div className="break-words text-muted">@{account.domain}</div>}
          </div>
          <div className="flex items-center justify-end">
            <IconButton
              title="Account security"
              onClick={() => setSecurityOpen(true)}
              size="md"
              icon={<Settings2 size={14} />}
            />
            <IconButton
              title="Sign out"
              onClick={async () => {
                try {
                  await post("/api/mail/logout");
                } finally {
                  clearCsrf();
                  onLoggedOut();
                }
              }}
              size="md"
              icon={<LogOut size={14} />}
            />
          </div>
        </div>
      </footer>
      {securityOpen && (
        <Modal title="Account security" onClose={() => setSecurityOpen(false)} wide>
          <AccountSecurity base="/api/mail" />
        </Modal>
      )}
    </aside>
  );
}
