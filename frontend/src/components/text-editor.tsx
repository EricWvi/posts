import { defaultKeymap, history, historyKeymap } from "@codemirror/commands";
import { Compartment, EditorState } from "@codemirror/state";
import { EditorView, drawSelection, keymap, lineNumbers } from "@codemirror/view";
import { useEffect, useRef, useState } from "react";

export default function TextEditor({ initialValue, onChange, readOnly }: { initialValue: string; onChange: (text: string) => void; readOnly: boolean }) {
  const host = useRef<HTMLDivElement>(null);
  const editor = useRef<EditorView | null>(null);
  const [editable] = useState(() => new Compartment());
  const change = useRef(onChange);
  change.current = onChange;
  useEffect(() => {
    const view = new EditorView({
      parent: host.current!,
      state: EditorState.create({
        doc: initialValue,
        extensions: [
          editable.of(EditorView.editable.of(true)),
          lineNumbers(),
          history(),
          drawSelection(),
          keymap.of([...defaultKeymap, ...historyKeymap]),
          EditorView.lineWrapping,
          EditorView.contentAttributes.of({ "aria-label": "Markdown 内容", spellcheck: "false" }),
          EditorView.updateListener.of((update) => {
            if (update.docChanged) change.current(update.state.doc.toString());
          }),
          EditorView.theme({
            "&": { height: "100%", backgroundColor: "var(--background)", color: "var(--foreground)" },
            ".cm-scroller": { overflow: "auto", fontFamily: "monospace", fontSize: "14px", lineHeight: "1.7" },
            ".cm-content": { padding: "12px 0", caretColor: "var(--foreground)" },
            ".cm-gutters": { backgroundColor: "var(--muted)", color: "var(--muted-foreground)", borderColor: "var(--border)" },
            "&.cm-focused": { outline: "none" },
            ".cm-cursor, .cm-dropCursor": { borderLeftColor: "var(--foreground)" },
            "&.cm-focused .cm-selectionBackground, .cm-selectionBackground, ::selection": { backgroundColor: "var(--accent)" },
          }),
        ],
      }),
    });
    editor.current = view;
    view.focus();
    return () => { editor.current = null; view.destroy(); };
  }, [initialValue, editable]);
  useEffect(() => {
    editor.current?.dispatch({ effects: editable.reconfigure(EditorView.editable.of(!readOnly)) });
  }, [readOnly, editable]);
  return <div ref={host} className="min-h-0 flex-1 overflow-hidden rounded-md border" />;
}
