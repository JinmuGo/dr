import { execFile, spawn } from "node:child_process";
import { promisify } from "node:util";
import type { ExtensionAPI } from "@oh-my-pi/pi-coding-agent";

const exec = promisify(execFile);

async function git(cwd: string, args: string[]): Promise<string> {
  const { stdout } = await exec("git", args, { cwd, timeout: 10_000 });
  return stdout.trim();
}

// Keep the base and changed-path rules aligned with internal/git/git.go.
async function defaultBranch(cwd: string): Promise<string> {
  try {
    return await git(cwd, ["symbolic-ref", "-q", "--short", "refs/remotes/origin/HEAD"]);
  } catch {
    for (const ref of ["refs/remotes/origin/main", "refs/remotes/origin/master", "refs/heads/main", "refs/heads/master"]) {
      try {
        await git(cwd, ["rev-parse", "--verify", "-q", ref]);
        return ref;
      } catch {
        // Try the next fallback, as dr does.
      }
    }
    return "HEAD";
  }
}

async function hasChanges(cwd: string, ref: string): Promise<boolean> {
  const base = await git(cwd, ["merge-base", "HEAD", ref]);
  try {
    await git(cwd, ["diff", "--quiet", base, "--"]);
  } catch (error) {
    if (error instanceof Error && "code" in error && error.code === 1) return true;
    throw error;
  }
  return (await git(cwd, ["ls-files", "--others", "--exclude-standard", "-z"])).length > 0;
}

export default function (pi: ExtensionAPI) {
  pi.registerCommand("dr", {
    description: "Review changes in dr; quit dr to return to this session",
    handler: async (args, ctx) => {
      if (ctx.mode !== "tui") {
        ctx.ui.notify("/dr needs an interactive OMP terminal.", "error");
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
          const base = await defaultBranch(cwd);
          const candidates = [];
          for (const worktree of worktrees) {
            if (await hasChanges(worktree.path, base)) candidates.push(worktree);
          }
          if (candidates.length === 0) {
            ctx.ui.notify("No changes to review in this repository's worktrees.", "info");
            return;
          }
          cwd = candidates[0].path;
          if (candidates.length > 1) {
            const selected = await ctx.ui.select("Review changes in which worktree?", candidates.map(worktree => worktree.label));
            if (selected === undefined) return;
            cwd = candidates.find(worktree => worktree.label === selected)!.path;
            if (!(await hasChanges(cwd, base))) {
              ctx.ui.notify("This worktree no longer has changes to review.", "info");
              return;
            }
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
