import { useRef, useEffect } from "react";
import { EditorState } from "@codemirror/state";
import { EditorView, keymap } from "@codemirror/view";
import { basicSetup } from "codemirror";
import { oneDark } from "@codemirror/theme-one-dark";
import { yaml } from "@codemirror/lang-yaml";
import { defaultKeymap } from "@codemirror/commands";

interface CodeViewerProps {
  value: string;
  onChange?: (value: string) => void;
  language?: "yaml" | "json" | "markdown";
  readOnly?: boolean;
  height?: string;
  placeholder?: string;
}

function getLanguageExtension(language: string | undefined) {
  switch (language) {
    case "yaml":
      return yaml();
    default:
      // json and markdown don't have dedicated extensions installed;
      // fall through to plain-text mode.
      return [];
  }
}

export function CodeViewer({
  value,
  onChange,
  language,
  readOnly = false,
  height = "300px",
  placeholder: _placeholder,
}: CodeViewerProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const viewRef = useRef<EditorView | null>(null);
  // Track the latest onChange in a ref so we don't recreate the editor when it changes.
  const onChangeRef = useRef(onChange);
  onChangeRef.current = onChange;

  // Track the latest external value to avoid echoing dispatches back.
  const externalValueRef = useRef(value);

  useEffect(() => {
    if (!containerRef.current) return;

    const isReadOnly = readOnly || !onChange;

    const extensions = [
      basicSetup,
      oneDark,
      keymap.of(defaultKeymap),
      getLanguageExtension(language),
      EditorView.theme({
        "&": { height, overflow: "auto" },
        ".cm-scroller": { overflow: "auto" },
      }),
    ];

    if (isReadOnly) {
      extensions.push(EditorState.readOnly.of(true));
      extensions.push(EditorView.editable.of(false));
    } else {
      extensions.push(
        EditorView.updateListener.of((update) => {
          if (update.docChanged) {
            const newValue = update.state.doc.toString();
            externalValueRef.current = newValue;
            onChangeRef.current?.(newValue);
          }
        })
      );
    }

    if (_placeholder) {
      // Use a DOM-level placeholder via theme instead of importing @codemirror/view placeholder
      // which may not be available. The placeholder extension is in @codemirror/view but
      // we handle it simply via the initial doc content hint.
    }

    const state = EditorState.create({
      doc: value,
      extensions,
    });

    const view = new EditorView({
      state,
      parent: containerRef.current,
    });

    viewRef.current = view;

    return () => {
      view.destroy();
      viewRef.current = null;
    };
    // Intentionally only run on mount / when readOnly or language changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [readOnly, language, height, !onChange]);

  // Sync external value changes into the editor.
  useEffect(() => {
    const view = viewRef.current;
    if (!view) return;

    // Only update if the external value differs from what the editor currently has.
    const currentDoc = view.state.doc.toString();
    if (value !== currentDoc && value !== externalValueRef.current) {
      externalValueRef.current = value;
      view.dispatch({
        changes: {
          from: 0,
          to: currentDoc.length,
          insert: value,
        },
      });
    }
  }, [value]);

  return (
    <div
      ref={containerRef}
      className="overflow-hidden rounded-md border border-border bg-[#282c34]"
      data-placeholder={_placeholder}
    />
  );
}
