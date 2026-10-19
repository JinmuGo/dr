import { execFile, spawn } from "node:child_process";
import { promisify } from "node:util";
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";

const exec = promisify(execFile);

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
        let cwd = ctx.cwd;
        if (!ref) {
          const { stdout } = await exec("git", ["worktree", "list", "--porcelain", "-z"], {
            cwd,
            timeout: 10_000,
          });
          const worktrees = stdout.split("\0\0").flatMap(record => {
            const fields = record.split("\0");
            const path = fields.find(field => field.startsWith("worktree "))?.slice(9);
            if (!path || fields.includes("bare") || fields.some(field => field.startsWith("prunable"))) return [];
            const branch = fields.find(field => field.startsWith("branch "))?.slice(7).replace(/^refs\/heads\//, "");
            return [{ path, label: `${branch ?? "(detached)"} — ${path}` }];
          });
          if (worktrees.length > 1) {
            const selected = await ctx.ui.select("Open dr in which worktree?", worktrees.map(worktree => worktree.label));
            if (selected === undefined) return;
            cwd = worktrees.find(worktree => worktree.label === selected)!.path;
          }
        }
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
