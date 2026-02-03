"use client";
import dynamic from "next/dynamic";

type NodeTerminalProps = {
  nodeId: string;
  labId: string;
  onClose?: () => void;
  expanded?: boolean;
  hideHeader?: boolean;
};

// Dynamically import xterm to avoid SSR issues
const TerminalComponent = dynamic(() => import("./terminal-inner"), {
  ssr: false,
  loading: () => (
    <div className="flex h-full items-center justify-center bg-slate-950 text-slate-500">
      Loading terminal...
    </div>
  ),
});

export function NodeTerminal({ nodeId, labId, onClose, expanded = false, hideHeader = false }: NodeTerminalProps) {
  return (
    <div className="flex h-full flex-col overflow-hidden" style={{ minHeight: 0 }}>
      {!hideHeader && (
        <div className="flex-shrink-0 flex items-center justify-between border-b border-slate-800 px-3 py-2">
          <div className="flex items-center gap-2">
            <span className="text-xs font-medium text-slate-400">Terminal</span>
          </div>
          {onClose && (
            <button
              onClick={onClose}
              className="text-xs text-slate-400 hover:text-white"
            >
              Close
            </button>
          )}
        </div>
      )}
      <div className="flex-1 overflow-hidden" style={{ minHeight: 0 }}>
        <TerminalComponent nodeId={nodeId} labId={labId} expanded={expanded} />
      </div>
    </div>
  );
}
