import { Card, CopyButton, InfoNote } from "../../../components/ui";

const commands = [
  {
    cmd: "wispboxctl backup create /var/backups/wispbox-$(date +%F).tar.gz",
    what: "Create a backup: control database, certificates, DKIM keys, and the instance secret.",
  },
  {
    cmd: "rsync -a /var/lib/wispbox/mail/ backup-host:/backups/wispbox-mail/",
    what: "Mail itself is plain Maildir files — rsync (or any file backup tool) is the right way to copy it.",
  },
  {
    cmd: "wispboxctl backup restore /var/backups/wispbox-2026-07-05.tar.gz",
    what: "Restore on a fresh server after running the installer. Stop wispboxd first, restart it after.",
  },
];

export default function Backup() {
  return (
    <div className="space-y-5">
      <InfoNote>
        Run backups on the server command line; backup files contain keys and credentials.
      </InfoNote>

      <Card title="What to back up">
        <ul className="space-y-1.5 text-[13px] leading-relaxed text-muted">
          <li>
            <span className="font-medium text-ink">Control data</span> — SQLite database, TLS
            certificates, DKIM keys, instance secret. Covered by{" "}
            <code className="rounded bg-inset px-1 font-mono text-[12px]">wispboxctl backup create</code>.
          </li>
          <li>
            <span className="font-medium text-ink">Mail storage</span> — Maildir under{" "}
            <code className="rounded bg-inset px-1 font-mono text-[12px]">/var/lib/wispbox/mail</code>. Plain
            files; back up with rsync, restic, borg, or whatever you already use.
          </li>
        </ul>
      </Card>

      <Card title="Commands">
        <div className="space-y-3">
          {commands.map((c) => (
            <div key={c.cmd}>
              <div className="flex items-center gap-1 rounded-lg border border-line bg-bg-deep px-3 py-2">
                <code className="flex-1 select-all break-all font-mono text-[12px] text-accent">
                  $ {c.cmd}
                </code>
                <CopyButton text={c.cmd} />
              </div>
              <p className="mt-1.5 text-[12px] leading-relaxed text-faint">{c.what}</p>
            </div>
          ))}
        </div>
      </Card>
    </div>
  );
}
