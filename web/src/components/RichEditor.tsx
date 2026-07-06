// A dependency-free rich-text editor built on the browser's native
// contentEditable. It produces HTML (for the message's text/html part) and a
// plain-text derivation (for the text/plain alternative). No third-party
// editor library — keeps the webmail bundle tiny, in line with wispbox's
// lightweight ethos. The produced HTML is always sanitized again server-side
// before it is sent, so this component is about authoring, not trust.
import { useEffect, useRef, useState } from "react";
import {
  Bold,
  Italic,
  Link2,
  List,
  ListOrdered,
  Quote,
  Redo2,
  RemoveFormatting,
  Strikethrough,
  Underline,
  Undo2,
} from "lucide-react";
import { IconButton } from "./ui";

type Cmd = {
  icon: typeof Bold;
  title: string;
  run: () => void;
  block?: string; // for formatBlock toggles, the tag it produces
};

export function RichEditor({
  initialHTML,
  placeholder,
  autoFocus,
  onChange,
}: {
  initialHTML: string;
  placeholder?: string;
  autoFocus?: boolean;
  onChange: (html: string, text: string) => void;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const [empty, setEmpty] = useState(!stripToText(initialHTML));

  // Seed once; React must not re-render the editable node's children or the
  // caret jumps, so contentEditable content is managed imperatively.
  useEffect(() => {
    if (ref.current) {
      ref.current.innerHTML = initialHTML || "";
      setEmpty(!ref.current.textContent?.trim());
      if (autoFocus) placeCaretAtStart(ref.current);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  function emit() {
    const el = ref.current;
    if (!el) return;
    setEmpty(!el.textContent?.trim());
    onChange(el.innerHTML, deriveText(el));
  }

  function exec(command: string, value?: string) {
    ref.current?.focus();
    // execCommand is deprecated but universally supported and dependency-free;
    // the pragmatic choice for a lightweight webmail composer.
    document.execCommand(command, false, value);
    emit();
  }

  function toggleBlock(tag: string) {
    // formatBlock toggles: if already in that block, revert to a paragraph.
    const current = document.queryCommandValue("formatBlock");
    exec("formatBlock", current.toLowerCase() === tag ? "p" : tag);
  }

  function addLink() {
    const url = window.prompt("Link URL", "https://");
    if (!url) return;
    if (!/^(https?:|mailto:)/i.test(url)) {
      window.alert("Links must start with http://, https://, or mailto:");
      return;
    }
    exec("createLink", url);
  }

  const groups: Cmd[][] = [
    [
      { icon: Bold, title: "Bold (⌘B)", run: () => exec("bold") },
      { icon: Italic, title: "Italic (⌘I)", run: () => exec("italic") },
      { icon: Underline, title: "Underline (⌘U)", run: () => exec("underline") },
      { icon: Strikethrough, title: "Strikethrough", run: () => exec("strikeThrough") },
    ],
    [
      { icon: List, title: "Bulleted list", run: () => exec("insertUnorderedList") },
      { icon: ListOrdered, title: "Numbered list", run: () => exec("insertOrderedList") },
      { icon: Quote, title: "Quote", run: () => toggleBlock("blockquote") },
      { icon: Link2, title: "Insert link", run: addLink },
    ],
    [
      { icon: RemoveFormatting, title: "Clear formatting", run: () => exec("removeFormat") },
      { icon: Undo2, title: "Undo", run: () => exec("undo") },
      { icon: Redo2, title: "Redo", run: () => exec("redo") },
    ],
  ];

  return (
    <div className="rounded-lg border border-line bg-inset focus-within:border-accent/50 focus-within:ring-2 focus-within:ring-accent/25">
      <div className="flex flex-wrap items-center gap-0.5 border-b border-line px-1.5 py-1">
        {groups.map((g, gi) => (
          <div key={gi} className="flex items-center gap-0.5">
            {gi > 0 && <span className="mx-1 h-4 w-px bg-line" />}
            {g.map((c) => {
              const Icon = c.icon;
              return (
                <IconButton
                  key={c.title}
                  title={c.title}
                  onMouseDown={(e) => e.preventDefault()} // keep selection
                  onClick={c.run}
                  icon={<Icon size={14} />}
                />
              );
            })}
          </div>
        ))}
      </div>
      <div className="relative">
        {empty && placeholder && (
          <div className="pointer-events-none absolute left-3 top-2.5 text-[13.5px] text-faint">
            {placeholder}
          </div>
        )}
        <div
          ref={ref}
          contentEditable
          role="textbox"
          aria-multiline="true"
          suppressContentEditableWarning
          onInput={emit}
          onBlur={emit}
          className="wisp-richtext min-h-[240px] w-full overflow-y-auto px-3 py-2.5 text-[13.5px] leading-relaxed text-ink focus:outline-none"
        />
      </div>
    </div>
  );
}

// deriveText produces a readable plain-text version of the editor content for
// the text/plain alternative. innerText already respects line breaks and list
// rendering, which is exactly what we want.
function deriveText(el: HTMLElement): string {
  return (el.innerText || "").replace(/\n{3,}/g, "\n\n").trim();
}

function stripToText(html: string): string {
  const d = document.createElement("div");
  d.innerHTML = html;
  return (d.textContent || "").trim();
}

function placeCaretAtStart(el: HTMLElement) {
  el.focus();
  const range = document.createRange();
  range.selectNodeContents(el);
  range.collapse(true);
  const sel = window.getSelection();
  sel?.removeAllRanges();
  sel?.addRange(range);
}
