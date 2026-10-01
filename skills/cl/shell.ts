// shell.ts list | show [name] | where [name] — read-only `cl` for the terminal (`! cl show`).
// Finds this repo's CLs without a session: <root>/<slug>/*/scratchpad/cl/*.json, where
// root = $CL_SCRATCH_ROOT or /private/tmp/claude-<uid> and slug = cwd with non-alphanumerics as "-".
import { join } from "node:path";
import { userInfo } from "node:os";
import { CliError, fail } from "./model.ts";
import { scan, show, where } from "./find.ts";
import { list } from "./render.ts";

function main(): void {
  const [cmd = "show", name] = process.argv.slice(2);
  const root = process.env.CL_SCRATCH_ROOT ?? `/private/tmp/claude-${userInfo().uid}`;
  const slug = process.cwd().replace(/[^A-Za-z0-9]/g, "-");
  const sp = join(root, slug, "-", "scratchpad"); // no own session: every CL counts as "other"
  if (cmd === "list") return console.log(list(scan(sp)));
  if (cmd !== "show" && cmd !== "where") fail("usage: cl list | show [name] | where [name]");
  const pick = name ?? scan(sp)[0]?.cl.name;
  if (!pick) fail(`no checklists for ${process.cwd()}`);
  console.log(cmd === "show" ? show(sp, pick) : where(sp, pick));
}

try {
  main();
} catch (e) {
  if (!(e instanceof CliError)) throw e;
  console.error(e.message);
  process.exit(1);
}
