import { useCallback, useEffect, useRef, useState } from "react";
import { FixedSizeList as List } from "react-window";
import { ArrowDown } from "lucide-react";
import { Button } from "@/components/ui/button";

interface TerminalViewerProps {
  lines: string[];
  streaming: boolean;
  maxLines?: number;
}

const ROW_HEIGHT = 20;
const SCROLL_THRESHOLD = 60; // pixels from bottom to consider "at bottom"

export function TerminalViewer({ lines, streaming, maxLines = 5000 }: TerminalViewerProps) {
  const listRef = useRef<List>(null);
  const outerRef = useRef<HTMLDivElement>(null);
  const [userScrolledUp, setUserScrolledUp] = useState(false);
  const isAutoScrolling = useRef(false);

  // Truncate from front when over maxLines
  const offset = lines.length > maxLines ? lines.length - maxLines : 0;
  const visibleLines = lines.length > maxLines ? lines.slice(-maxLines) : lines;

  const handleScroll = useCallback(
    ({ scrollOffset, scrollDirection }: { scrollOffset: number; scrollDirection: "forward" | "backward" }) => {
      if (isAutoScrolling.current) return;
      if (!outerRef.current) return;

      const totalHeight = visibleLines.length * ROW_HEIGHT;
      const viewportHeight = outerRef.current.clientHeight;
      const distFromBottom = totalHeight - scrollOffset - viewportHeight;

      if (scrollDirection === "backward" && distFromBottom > SCROLL_THRESHOLD) {
        setUserScrolledUp(true);
      } else if (distFromBottom <= SCROLL_THRESHOLD) {
        setUserScrolledUp(false);
      }
    },
    [visibleLines.length],
  );

  // Auto-scroll to bottom when streaming and user hasn't scrolled up
  useEffect(() => {
    if (streaming && !userScrolledUp && listRef.current && visibleLines.length > 0) {
      isAutoScrolling.current = true;
      listRef.current.scrollToItem(visibleLines.length - 1, "end");
      // Reset the flag after a tick so the scroll handler doesn't interpret it as user scroll
      requestAnimationFrame(() => {
        isAutoScrolling.current = false;
      });
    }
  }, [visibleLines.length, streaming, userScrolledUp]);

  function jumpToBottom() {
    if (listRef.current && visibleLines.length > 0) {
      isAutoScrolling.current = true;
      listRef.current.scrollToItem(visibleLines.length - 1, "end");
      setUserScrolledUp(false);
      requestAnimationFrame(() => {
        isAutoScrolling.current = false;
      });
    }
  }

  const gutterWidth = String(visibleLines.length + offset).length;

  return (
    <div className="relative flex flex-col" style={{ height: "100%" }}>
      <div
        className="flex-1 rounded-lg overflow-hidden"
        style={{ backgroundColor: "#1a1a2e" }}
      >
        <List
          ref={listRef}
          outerRef={outerRef}
          height={400}
          width="100%"
          itemCount={visibleLines.length}
          itemSize={ROW_HEIGHT}
          onScroll={handleScroll}
          style={{ fontFamily: "ui-monospace, SFMono-Regular, 'SF Mono', Menlo, monospace" }}
        >
          {({ index, style }) => {
            const lineNum = index + offset + 1;
            return (
              <div
                style={style}
                className="flex items-center text-xs leading-5 hover:bg-white/5"
              >
                <span
                  className="select-none text-right pr-3 pl-2"
                  style={{
                    width: `${(gutterWidth + 2) * 0.6}em`,
                    color: "#4a4a6a",
                    minWidth: "3em",
                  }}
                >
                  {lineNum}
                </span>
                <span
                  className="flex-1 whitespace-pre pr-4"
                  style={{ color: "#e0e0e0" }}
                >
                  {visibleLines[index]}
                </span>
              </div>
            );
          }}
        </List>
      </div>

      {/* Jump to bottom button */}
      {userScrolledUp && streaming && (
        <div className="absolute bottom-4 left-1/2 -translate-x-1/2">
          <Button
            variant="secondary"
            size="sm"
            onClick={jumpToBottom}
            className="shadow-lg gap-1"
          >
            <ArrowDown className="size-3" />
            Jump to bottom
          </Button>
        </div>
      )}
    </div>
  );
}
