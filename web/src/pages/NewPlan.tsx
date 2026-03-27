import { useState, useRef, useCallback } from "react";
import { useNavigate } from "react-router-dom";
import { submitPlan } from "@/api/client";
import { CodeViewer } from "@/components/CodeViewer";
import { Button } from "@/components/ui/button";

export default function NewPlan() {
  const navigate = useNavigate();
  const [specContent, setSpecContent] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);

  const handleLoadFile = useCallback(() => {
    fileInputRef.current?.click();
  }, []);

  const handleFileChange = useCallback((e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;
    const reader = new FileReader();
    reader.onload = () => {
      if (typeof reader.result === "string") {
        setSpecContent(reader.result);
      }
    };
    reader.readAsText(file);
    // Reset so the same file can be re-selected
    e.target.value = "";
  }, []);

  const handleGenerate = useCallback(async () => {
    if (!specContent.trim()) return;

    setSubmitting(true);
    setError(null);

    try {
      const result = await submitPlan({ spec_content: specContent });
      // Use plan_run_id if available, fall back to session_id for backward compatibility
      const id = result.plan_run_id || result.session_id;
      navigate(`/plan/${encodeURIComponent(id)}`);
    } catch (err) {
      const message = err instanceof Error ? err.message : "Failed to submit plan";
      setError(message);
    } finally {
      setSubmitting(false);
    }
  }, [specContent, navigate]);

  return (
    <div className="flex flex-col gap-4 p-6">
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold">New Plan</h2>
        <div className="flex items-center gap-2">
          <input
            ref={fileInputRef}
            type="file"
            accept=".md"
            className="hidden"
            onChange={handleFileChange}
          />
          <Button variant="outline" size="sm" onClick={handleLoadFile}>
            Load File
          </Button>
          <Button
            variant="default"
            size="sm"
            onClick={handleGenerate}
            disabled={submitting || !specContent.trim()}
          >
            {submitting ? (
              <span className="flex items-center gap-2">
                <span className="inline-block h-4 w-4 animate-spin rounded-full border-2 border-current border-t-transparent" />
                Submitting...
              </span>
            ) : (
              "Generate Plan"
            )}
          </Button>
        </div>
      </div>

      <CodeViewer
        value={specContent}
        onChange={setSpecContent}
        language="markdown"
        height="60vh"
        placeholder="Paste your feature spec here..."
      />

      {error && (
        <p className="text-sm text-red-400">{error}</p>
      )}
    </div>
  );
}
