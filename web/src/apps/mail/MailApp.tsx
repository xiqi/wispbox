import { useCallback, useEffect, useRef, useState } from "react";
import { ApiError, get, setCsrf } from "../../lib/api";
import { useBrand } from "../../lib/brand";
import type { Folder, MessageHeader, MessagePage } from "../../lib/types";
import { Spinner } from "../../components/ui";
import Login from "./Login";
import Sidebar from "./Sidebar";
import MessageList from "./MessageList";
import MessageView from "./MessageView";
import Compose, { type ComposeSeed } from "./Compose";

type MobilePane = "folders" | "list" | "message";

export default function MailApp() {
  const brand = useBrand();
  const [me, setMe] = useState<string | null | undefined>(undefined);
  const [folders, setFolders] = useState<Folder[]>([]);
  const [folder, setFolder] = useState("INBOX");
  const [query, setQuery] = useState("");
  const [messages, setMessages] = useState<MessageHeader[]>([]);
  const [nextCursor, setNextCursor] = useState<string | undefined>();
  const [total, setTotal] = useState(0);
  const [listBusy, setListBusy] = useState(false);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [compose, setCompose] = useState<ComposeSeed | null>(null);
  const [listError, setListError] = useState("");
  const [mobilePane, setMobilePane] = useState<MobilePane>("list");

  // Session probe. The CSRF token is stateless (an HMAC of the session), so
  // the server hands it back here — this is what keeps mutations working
  // after a browser restart, when sessionStorage has been cleared but the
  // 7-day session cookie is still valid.
  useEffect(() => {
    document.title = brand.name;
  }, [brand.name]);

  useEffect(() => {
    get<{ email: string; csrf: string }>("/api/mail/me")
      .then((r) => {
        if (r.csrf) setCsrf(r.csrf);
        setMe(r.email);
      })
      .catch(() => setMe(null));
  }, []);

  const loadFolders = useCallback(async () => {
    try {
      const r = await get<{ folders: Folder[] }>("/api/mail/folders");
      setFolders(r.folders ?? []);
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) setMe(null);
    }
  }, []);

  // Monotonic request id: a slow response for an old query/folder must never
  // overwrite the list the user is now looking at.
  const listReqRef = useRef(0);
  const loadMessages = useCallback(
    async (target: string, q: string, cursor?: string) => {
      const reqId = ++listReqRef.current;
      setListBusy(true);
      setListError("");
      try {
        const params = new URLSearchParams({ folder: target });
        if (q) params.set("q", q);
        if (cursor) params.set("cursor", cursor);
        const page = await get<MessagePage>(`/api/mail/messages?${params}`);
        if (reqId !== listReqRef.current) return; // superseded
        setMessages((prev) => (cursor ? [...prev, ...page.messages] : page.messages));
        setNextCursor(page.next_cursor);
        setTotal(page.total);
      } catch (err: any) {
        if (reqId !== listReqRef.current) return;
        if (err instanceof ApiError && err.status === 401) {
          setMe(null);
          return;
        }
        setListError(err.message);
      } finally {
        if (reqId === listReqRef.current) setListBusy(false);
      }
    },
    [],
  );

  // Load folders once per folder change (not per keystroke) and debounce the
  // message query so typing a search fires one request, not one per letter.
  useEffect(() => {
    if (!me) return;
    loadFolders();
  }, [me, folder, loadFolders]);

  useEffect(() => {
    if (!me) return;
    const t = setTimeout(() => loadMessages(folder, query), query ? 280 : 0);
    return () => clearTimeout(t);
  }, [me, folder, query, loadMessages]);

  const refresh = useCallback(() => {
    loadFolders();
    loadMessages(folder, query);
  }, [folder, query, loadFolders, loadMessages]);

  if (me === undefined) return <Spinner label="Loading mail…" />;
  if (me === null)
    return (
      <Login
        onLogin={(email) => {
          setMe(email);
          setFolder("INBOX");
          setSelectedId(null);
          setMobilePane("list");
        }}
      />
    );

  return (
    <div className="flex h-full overflow-hidden bg-bg">
      <Sidebar
        me={me}
        folders={folders}
        active={folder}
        onSelect={(f) => {
          setFolder(f);
          setQuery("");
          setSelectedId(null);
          setMobilePane("list");
        }}
        onCompose={() => setCompose({ mode: "new" })}
        onLoggedOut={() => setMe(null)}
        hidden={mobilePane !== "folders"}
      />
      <MessageList
        folder={folder}
        messages={messages}
        total={total}
        busy={listBusy}
        error={listError}
        query={query}
        onQuery={setQuery}
        selectedId={selectedId}
        onFolders={() => setMobilePane("folders")}
        onSelect={(m) => {
          setSelectedId(m.id);
          setMobilePane("message");
          if (!m.seen) {
            // optimistic unread badge update
            setMessages((prev) => prev.map((x) => (x.id === m.id ? { ...x, seen: true } : x)));
          }
        }}
        nextCursor={nextCursor}
        onLoadMore={() => loadMessages(folder, query, nextCursor)}
        onRefresh={refresh}
        hidden={mobilePane !== "list"}
      />
      <MessageView
        id={selectedId}
        folders={folders}
        onClose={() => {
          setSelectedId(null);
          setMobilePane("list");
          refresh();
        }}
        onGone={() => {
          setSelectedId(null);
          setMobilePane("list");
          refresh();
        }}
        onCompose={setCompose}
      />
      {compose && (
        <Compose
          seed={compose}
          me={me}
          onClose={() => setCompose(null)}
          onSent={() => {
            setCompose(null);
            refresh();
          }}
        />
      )}
    </div>
  );
}
