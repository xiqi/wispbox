import { Loader2, Play, RefreshCw, Trash2, Zap } from "lucide-react";
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

const loadQueue = () => get<{ items: QueueItem[] }>("/api/admin/queue");

export default function Queue() {
  const { data, error, busy, reload, setData } = useLoad(loadQueue);
  const [deleting, setDeleting] = useState<QueueItem | null>(null);
  const [refreshing, setRefreshing] = useState(false);
  const [flushing, setFlushing] = useState(false);
  const [retrying, setRetrying] = useState("");

  async function refreshQueue(message = "Queue refreshed") {
    setRefreshing(true);
    try {
      const next = await loadQueue();
      setData(next);
      toast(message);
    } catch (e: any) {
      toast(e.message, "error");
    } finally {
      setRefreshing(false);
    }
  }

  async function retryAll() {
    setFlushing(true);
    try {
      await post("/api/admin/queue/flush");
      const next = await loadQueue();
      setData(next);
      toast(next.items.length === 0 ? "Queue is empty" : "Retry started for queued mail");
    } catch (e: any) {
      toast(e.message, "error");
    } finally {
      setFlushing(false);
    }
  }

  async function retryOne(queueID: string) {
    setRetrying(queueID);
    try {
      await post(`/api/admin/queue/${queueID}/retry`);
      const next = await loadQueue();
      setData(next);
      toast("Retry started");
    } catch (e: any) {
      toast(e.message, "error");
    } finally {
      setRetrying("");
    }
  }

  return (
    <div className="space-y-4">
      <div className="flex justify-end">
        <div className="flex gap-2">
          <Button size="sm" onClick={() => refreshQueue()} busy={refreshing} disabled={busy || flushing}>
            {!refreshing && <RefreshCw size={13} />} Refresh
          </Button>
          <Button
            size="sm"
            variant="primary"
            onClick={retryAll}
            busy={flushing}
            disabled={busy || refreshing}
          >
            {!flushing && <Zap size={13} />} Retry all now
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
                  <Th>Sender and recipients</Th>
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
                        disabled={retrying === q.queue_id || flushing}
                        icon={
                          retrying === q.queue_id ? (
                            <Loader2 size={14} className="animate-spin" />
                          ) : (
                            <Play size={14} />
                          )
                        }
                        onClick={() => retryOne(q.queue_id)}
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
