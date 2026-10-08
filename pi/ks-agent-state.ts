// ks agent-state extension for pi: reports pi's lifecycle to ks, the kitty
// session manager, so the sidebar shows the state of the session this pi
// runs in. ks has no other way to see pi: pi has no hooks and sets no title
// glyph. Every event runs `ks _pi-hook` with a JSON payload on stdin; the
// event table and the install steps are in docs/pi.md.
//
// Lives in the kitty-session repo as pi/ks-agent-state.ts and is symlinked
// into ~/.pi/agent/extensions/ by the dotfiles recipe. No build step.
// @ts-nocheck

import { execFile } from "node:child_process";
import path from "node:path";

// The launcher exports KS_SESSION_ID into both windows of a session; outside
// one the extension does nothing. KS_EXE names the ks binary that launched
// the session so the hook runs the same build; a bare ks on PATH otherwise.
const sessionId = process.env.KS_SESSION_ID;
const ksExe = process.env.KS_EXE ?? "ks";
// One hook run is a file write; a hung ks must not hold pi's exit.
const hookTimeoutMs = 5000;

type Payload = {
  event: string;
  session_id?: string;
  session_file?: string;
  reason?: string;
  label?: string;
};

function enabled(): boolean {
  return typeof sessionId === "string" && sessionId.length > 0;
}

// runHook runs one `ks _pi-hook` with the payload on stdin and resolves when
// it exits, whatever happened: a missing binary, a non-zero exit or a closed
// pipe must never surface in pi.
function runHook(payload: Payload): Promise<void> {
  return new Promise((resolve) => {
    let child;
    try {
      child = execFile(ksExe, ["_pi-hook"], { timeout: hookTimeoutMs }, () => resolve());
    } catch {
      resolve();
      return;
    }
    child.on("error", () => resolve());
    child.stdin?.on("error", () => {});
    child.stdin?.end(JSON.stringify(payload));
  });
}

// Hook runs are serialized so state writes land in event order.
let queue: Promise<void> = Promise.resolve();

function send(payload: Payload): Promise<void> {
  const next = queue.then(() => runHook(payload)).catch(() => {});
  queue = next;
  return next;
}

let currentSessionId: string | undefined;
let currentSessionFile: string | undefined;

// updateSessionRef reads pi's session id and file from the context. Both
// accessors are version dependent, so a missing one leaves the field unset.
function updateSessionRef(ctx: any): void {
  try {
    const file = ctx?.sessionManager?.getSessionFile?.();
    currentSessionFile = typeof file === "string" && path.isAbsolute(file) ? file : undefined;
  } catch {
    currentSessionFile = undefined;
  }
  try {
    const id = ctx?.sessionManager?.getSessionId?.();
    currentSessionId = typeof id === "string" && id.length > 0 ? id : undefined;
  } catch {
    currentSessionId = undefined;
  }
}

function report(event: string, extra: Partial<Payload> = {}): Promise<void> {
  return send({
    event,
    session_id: currentSessionId,
    session_file: currentSessionFile,
    ...extra,
  });
}

export default function (pi) {
  if (!enabled()) {
    return;
  }

  // rootSession is set once session_start arrives from pi's TUI; every other
  // handler is a no-op before that.
  let rootSession = false;
  let agentActive = false;
  // blockedCount tracks the permission prompts waiting on the user; the row
  // goes back to working only when the last one is answered.
  let blockedCount = 0;

  pi.on("session_start", (event, ctx) => {
    // TUI only: a nested `pi -p` started from inside the session inherits
    // KS_SESSION_ID but runs headless, and RPC/JSON/print modes have no tab
    // of their own. mode is the reliable gate; RPC still reports hasUI.
    if (ctx?.mode !== "tui") {
      return;
    }
    rootSession = true;
    blockedCount = 0;
    updateSessionRef(ctx);
    void report("session_start", { reason: event?.reason });
    // A reload can replace this extension mid-run without another agent_start.
    agentActive = ctx?.isIdle?.() === false;
    if (agentActive) {
      void report("agent_start");
    }
  });

  pi.on("agent_start", (_event, ctx) => {
    if (!rootSession) {
      return;
    }
    updateSessionRef(ctx);
    agentActive = true;
    void report("agent_start");
  });

  // tool_call is the keepalive: a fresh working write every tool call keeps
  // the state file inside the sidebar's freshness window, like claude's
  // PreToolUse hook. Skipped while a prompt waits so it cannot bury the input.
  pi.on("tool_call", () => {
    if (!rootSession || blockedCount > 0) {
      return;
    }
    void report("tool_call");
  });

  pi.on("agent_settled", (_event, ctx) => {
    if (!rootSession || ctx?.isIdle?.() !== true) {
      return;
    }
    agentActive = false;
    void report("agent_settled");
  });

  // Awaited: pi exits right after its shutdown handlers return, so the hook
  // must have run by then. reason is quit, reload, new, resume or fork, and
  // pi reports quit for a SIGHUP too, so ks marks nothing stopped on it.
  pi.on("session_shutdown", async (event) => {
    if (!rootSession) {
      return;
    }
    rootSession = false;
    agentActive = false;
    blockedCount = 0;
    await report("session_shutdown", { reason: event?.reason });
  });

  // herdr:blocked comes from the permission-gate extension while a tool call
  // waits on the user: active with a label when the prompt opens, inactive
  // when it is answered. A denied call still ends the run through
  // agent_settled, so unblocked only ever means working.
  pi.events?.on?.("herdr:blocked", (data) => {
    if (!rootSession) {
      return;
    }
    if (data?.active) {
      blockedCount += 1;
      void report("blocked", { label: data?.label });
      return;
    }
    blockedCount = Math.max(0, blockedCount - 1);
    if (blockedCount > 0) {
      return;
    }
    void report("unblocked");
  });
}
