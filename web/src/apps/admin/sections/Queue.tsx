import { Play, RefreshCw, Trash2, Zap } from "lucide-react";
import { useState } from "react";
import { del, get, post } from "../../../lib/api";
import { useLoad } from "../../../lib/hooks";
import type { QueueItem } from "../../../lib/types";
import { formatBytes, formatDate } from "../../../lib/format";
import {
  Button,
  Card,
  ConfirmDialog,
  EmptyState,
  ErrorNote,
  IconButton,
  Identifier,
  Spinner,
  StatusPill,
  Table,
  Td,
  Th,
  toast,
} from "../../../components/ui";

export default function Queue() {
  const { data, error, busy, reload } = useLoad(() =>
    get<{ items: QueueItem[] }>("/api/admin/queue"),
  );
  const [deleting, setDeleting] = useState<QueueItem | null>(null);
  const [flushing, setFlushing] = useState(false);

  return (
    <div className="space-y-4">
      <div className="flex justify-end">
        <div className="flex gap-2">
          <Button size="sm" onClick={reload}>
            <RefreshCw size={13} /> Refresh
          </Button>
          <Button
            size="sm"
            variant="primary"
            onClick={async () => {
              setFlushing(true);
              try {
                await post("/api/admin/queue/flush");
                toast("Delivery attempt triggered for all queued mail");
                setTimeout(reload, 1500);
              } catch (e: any) {
                toast(e.message, "error");
              } finally {
                setFlushing(false);
              }
            }}
            busy={flushing}
          >
            <Zap size={13} /> Retry all now
          </Button>
        </div>
      </div>

      {error && <ErrorNote>{error}</ErrorNote>}
      {busy && !data && <Spinner />}
      {data && (
        <Card>
          {data.items.length === 0 ? (
            <EmptyState title="Queue is empty" />
          ) : (
            <Table
              head={
                <>
                  <Th>Queue ID</Th>
                  <Th>From → To</Th>
                  <Th>State</Th>
                  <Th>Size</Th>
                  <Th>Arrived</Th>
                  <Th />
                </>
              }
            >
              {data.items.map((q) => (
                <tr key={q.queue_id} className="group">
                  <Td>
                    <Identifier muted className="text-[12px]">{q.queue_id}</Identifier>
                  </Td>
                  <Td>
                    <Identifier className="block text-[12px]">{q.sender}</Identifier>
                    <Identifier muted className="mt-1 block text-[12px]">
                      → {q.recipients.join(", ")}
                    </Identifier>
                    {q.reason && (
                      <div className="mt-1 max-w-md break-words text-[11.5px] leading-relaxed text-danger">
                        {q.reason}
                      </div>
                    )}
                  </Td>
                  <Td>
                    <StatusPill
                      status={q.queue === "deferred" ? "warn" : "ok"}
                      label={q.queue}
                    />
                  </Td>
                  <Td className="text-muted">{formatBytes(q.size_bytes)}</Td>
                  <Td className="text-muted">{formatDate(q.arrived_at)}</Td>
                  <Td className="text-right">
                    <div className="flex items-center justify-end gap-1 opacity-0 transition-opacity group-hover:opacity-100">
                      <IconButton
                        title="Retry this message"
                        tone="accent"
                        icon={<Play size={14} />}
                        onClick={async () => {
                          try {
                            await post(`/api/admin/queue/${q.queue_id}/retry`);
                            toast("Retry triggered");
                            setTimeout(reload, 1200);
                          } catch (e: any) {
                            toast(e.message, "error");
                          }
                        }}
                      />
                      <IconButton
                        title="Delete from queue"
                        tone="danger"
                        onClick={() => setDeleting(q)}
                        icon={<Trash2 size={14} />}
                      />
                    </div>
                  </Td>
                </tr>
              ))}
            </Table>
          )}
        </Card>
      )}
      {deleting && (
        <ConfirmDialog
          title={`Delete queued message ${deleting.queue_id}?`}
          confirmLabel="Delete message"
          danger
          onClose={() => setDeleting(null)}
          onConfirm={async () => {
            await del(`/api/admin/queue/${deleting.queue_id}`);
            toast("Message removed from queue");
            reload();
          }}
        >
          This removes the message from Postfix's queue. It will not be delivered to{" "}
          {deleting.recipients.join(", ")}.
        </ConfirmDialog>
      )}
    </div>
  );
}
