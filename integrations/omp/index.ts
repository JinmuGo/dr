import { spawn } from "node:child_process";
import type { ExtensionAPI } from "@oh-my-pi/pi-coding-agent";

export default function (pi: ExtensionAPI) {
  pi.registerCommand("dr", {
    description: "Review changes in dr; quit dr to return to this session",
    handler: async (args, ctx) => {
      if (ctx.mode !== "tui") {
        ctx.ui.notify("/dr needs an interactive OMP terminal.", "error");
        return;
      }
      if (args.trim()) {
        ctx.ui.notify("Usage: /dr (reviews the current working directory)", "error");
        return;
      }
      if (!ctx.isIdle()) {
        ctx.ui.notify("Wait for the current turn to finish before opening dr.", "warning");
        return;
      }

      try {
        await ctx.ui.custom<void>(async (tui, _theme, _keys, done) => {
          // Give dr the real terminal, not a captured tool-output stream.
          tui.stop();
          try {
            await new Promise<void>((resolve, reject) => {
              const child = spawn("dr", [], { cwd: ctx.cwd, stdio: "inherit" });
              child.once("error", reject);
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
        const message = (error as NodeJS.ErrnoException).code === "ENOENT"
          ? "dr was not found. Install dr and make it available on PATH."
          : error instanceof Error ? error.message : String(error);
        ctx.ui.notify(message, "error");
      }
    },
  });
}
