// tokens <session-id | dir>... [--by unit|group] [--json] [--root <dir>]
// Reads the arguments and chains locate -> aggregate / group -> render. No counting rule lives here.
import { homedir } from "node:os";
import { join } from "node:path";
import { aggregate } from "./aggregate.ts";
import { aggregateGroups } from "./group.ts";
import { locate } from "./locate.ts";
import { render, renderGroups } from "./render.ts";

const USAGE = "usage: tokens <session-id | dir>... [--by unit|group] [--json] [--root <dir>]   (--root defaults to ~/.claude/projects)";

function fail(message: string): never {
  console.error(`${message}\n${USAGE}`);
  process.exit(1);
}

const targets: string[] = [];
let json = false;
let by: "unit" | "group" = "unit";
let root = join(homedir(), ".claude", "projects");
const argv = process.argv.slice(2);
for (let i = 0; i < argv.length; i++) {
  if (argv[i] === "--json") json = true;
  else if (argv[i] === "--by") {
    const value = argv[++i];
    if (value !== "unit" && value !== "group") fail("--by takes unit or group");
    by = value;
  } else if (argv[i] === "--root") root = argv[++i] ?? fail("--root requires a directory");
  else targets.push(argv[i]);
}

if (targets.length === 0) fail("no session id or directory given");
const files = locate(targets, root);
if (files.length === 0) fail(`no transcript found for: ${targets.join(" ")}`);
console.log(by === "group" ? renderGroups(aggregateGroups(files), json) : render(aggregate(files), json));
