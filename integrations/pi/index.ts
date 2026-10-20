import { spawn } from "node:child_process";
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";

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
        await ctx.ui.custom<void>(async (tui, _theme, _keys, done) => {
          // Give dr the real terminal, not a captured tool-output stream.
          tui.stop();
          try {
            await new Promise<void>((resolve, reject) => {
              const child = spawn("dr", ref ? [ref] : [], { cwd, stdio: "inherit" });
              child.once("error", error => reject(new Error(`Could not start dr: ${error.message}. Check PATH and the worktree directory.`)));
              child.once("exit", (code, signal) => {
                if (code === 0) resolve();
                else reject(new Error(signal ? `dr stopped by ${signal}` : `dr exited with status ${code}`));
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
      } catch (error) {
        const message = error instanceof Error ? error.message : String(error);
        ctx.ui.notify(message, "error");
      }
    },
  });
}
