import { spawn } from "node:child_process";
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";

// lastFrame returns what fullscreen Pi shows now, for the main screen under dr, where dr's b
// shows it: when it stops, fullscreen Pi leaves only its footer there. Pi on the main screen
// leaves its transcript there itself and has no frame to give.
function lastFrame(tui: { renderNow(): void }): string {
  if (!("getScreenLines" in tui) || typeof tui.getScreenLines !== "function") return "";
  tui.renderNow(); // without an overlay that just closed
  const lines = tui.getScreenLines() as string[];
  return `\x1b[?7l${lines.join("\r\n")}\x1b[0m\x1b[?7h\r\n`;
}

// noChanges is the exit code dr is asked for when it has no changed file to show.
const noChanges = 3;

export default function (pi: ExtensionAPI) {
  pi.registerCommand("dr", {
    description: "Review changes in dr; quit dr to return to this session",
    handler: async (args, ctx) => {
      if (ctx.mode !== "tui") {
        ctx.ui.notify("/dr needs an interactive Pi terminal.", "error");
        return;
      }
      const ref = args.trim();
      if (!ctx.isIdle()) {
        ctx.ui.notify("Wait for the current turn to finish before opening dr.", "warning");
        return;
      }

      try {
        // dr asks which worktree to open when there are several and no ref is given.
        const cwd = ctx.cwd;
        let empty = false;
        await ctx.ui.custom<void>(async (tui, _theme, _keys, done) => {
          // Give dr the real terminal, not a captured tool-output stream.
          const frame = lastFrame(tui);
          tui.stop();
          process.stdout.write(frame);
          try {
            await new Promise<void>((resolve, reject) => {
              // With nothing to show dr only prints a line, which Pi draws over; it exits
              // with noChanges instead, for the notice below.
              const env = { ...process.env, DR_EMPTY_STATUS: String(noChanges) };
              const child = spawn("dr", ref ? [ref] : [], { cwd, env, stdio: "inherit" });
              child.once("error", error => reject(new Error(`Could not start dr: ${error.message}. Check PATH and the worktree directory.`)));
              child.once("exit", (code, signal) => {
                if (code === 0) resolve();
                else if (code === noChanges) {
                  empty = true;
                  resolve();
                } else reject(new Error(signal ? `dr stopped by ${signal}` : `dr exited with status ${code}`));
              });
            });
          } finally {
            // Restore input and repaint even when dr is missing or fails.
            tui.start();
            tui.requestRender(true);
          }
          done();
          return { render: () => [] };
        });
        if (empty) ctx.ui.notify(`No changed files in ${cwd}`, "info");
      } catch (error) {
        const message = error instanceof Error ? error.message : String(error);
        ctx.ui.notify(message, "error");
      }
    },
  });
}
