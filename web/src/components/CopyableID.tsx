import { useState, useCallback } from "react";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";

interface CopyableIDProps {
  id: string;
  truncate?: number;
}

export function CopyableID({ id, truncate = 8 }: CopyableIDProps) {
  const [copied, setCopied] = useState(false);

  const handleClick = useCallback(() => {
    navigator.clipboard.writeText(id).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    }).catch(() => {
      // Clipboard API unavailable (e.g. non-HTTPS context)
    });
  }, [id]);

  const display = id.length > truncate ? id.slice(0, truncate) : id;

  return (
    <TooltipProvider>
      <Tooltip>
        <TooltipTrigger asChild>
          <span
            role="button"
            tabIndex={0}
            className="cursor-pointer font-mono"
            onClick={handleClick}
            onKeyDown={(e) => {
              if (e.key === "Enter" || e.key === " ") {
                handleClick();
              }
            }}
          >
            {copied ? "Copied!" : display}
          </span>
        </TooltipTrigger>
        <TooltipContent>Click to copy</TooltipContent>
      </Tooltip>
    </TooltipProvider>
  );
}
