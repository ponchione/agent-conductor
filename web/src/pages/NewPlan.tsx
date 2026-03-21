import { useState, useRef, useCallback } from "react";
import { useNavigate } from "react-router-dom";
import { submitPlan } from "@/api/client";
import { CodeViewer } from "@/components/CodeViewer";
import { Button } from "@/components/ui/button";

export default function NewPlan() {
  const navigate = useNavigate();
  const [specContent, setSpecContent] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [progress, setProgress] = useState<string | null>(null);
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
    setProgress("Submitting spec...");
    setError(null);

    try {
      setProgress("Generating plan... this may take a moment.");
      const result = await submitPlan({ spec_content: specContent });
      setProgress("Plan generated successfully!");
      // Navigate to the new plan run detail after a brief delay
      setTimeout(() => {
        navigate(`/plan/${encodeURIComponent(result.session_id)}`);
      }, 500);
    } catch (err) {
      const message = err instanceof Error ? err.message : "Failed to generate plan";
      setError(message);
      setProgress(null);
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
                Generating...
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

      {progress && (
        <p className="text-sm text-muted-foreground">{progress}</p>
      )}
      {error && (
        <p className="text-sm text-red-400">{error}</p>
      )}
    </div>
  );
}
