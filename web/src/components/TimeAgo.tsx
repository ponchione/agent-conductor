import { useState, useEffect } from "react";

function formatRelativeTime(timestamp: string): string {
  const now = Date.now();
  const then = new Date(timestamp).getTime();
  const diffMs = now - then;
  const diffSeconds = Math.floor(diffMs / 1000);

  if (diffSeconds < 60) {
    return "just now";
  }

  const diffMinutes = Math.floor(diffSeconds / 60);
  if (diffMinutes < 60) {
    return `${diffMinutes} minute${diffMinutes === 1 ? "" : "s"} ago`;
  }

  const diffHours = Math.floor(diffMinutes / 60);
  if (diffHours < 24) {
    return `${diffHours} hour${diffHours === 1 ? "" : "s"} ago`;
  }

  if (diffHours < 48) {
    return "yesterday";
  }

  const diffDays = Math.floor(diffHours / 24);
  if (diffDays < 7) {
    return `${diffDays} days ago`;
  }

  const date = new Date(timestamp);
  const month = date.toLocaleString("en-US", { month: "short" });
  const day = date.getDate();
  return `${month} ${day}`;
}

interface TimeAgoProps {
  timestamp: string;
}

export function TimeAgo({ timestamp }: TimeAgoProps) {
  const [relative, setRelative] = useState(() => formatRelativeTime(timestamp));

  useEffect(() => {
    setRelative(formatRelativeTime(timestamp));

    const interval = setInterval(() => {
      setRelative(formatRelativeTime(timestamp));
    }, 30000);

    return () => clearInterval(interval);
  }, [timestamp]);

  return <span title={timestamp}>{relative}</span>;
}
