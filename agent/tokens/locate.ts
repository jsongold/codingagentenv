// locate: targets (session ids or transcript directories) -> transcript files.
// Decides where the files are and which unit each one is. Reads no transcript body;
// the only file content read is `description` in agent-*.meta.json.
import { existsSync, readdirSync, readFileSync, statSync } from "node:fs";
import { basename, join } from "node:path";

export type TranscriptFile = {
  path: string;
  unit: "session" | "subagent";
  session: string;
  agent?: string;
  description?: string;
};

const isDir = (p: string): boolean => existsSync(p) && statSync(p).isDirectory();
const jsonlIn = (dir: string): string[] =>
  isDir(dir) ? readdirSync(dir).filter((n) => n.endsWith(".jsonl")).map((n) => join(dir, n)) : [];
const stem = (path: string): string => basename(path, ".jsonl");

function description(path: string): string | undefined {
  try {
    const d = JSON.parse(readFileSync(path.replace(/\.jsonl$/, ".meta.json"), "utf8")).description;
    return typeof d === "string" ? d : undefined;
  } catch {
    return undefined;
  }
}

const sessionFile = (path: string): TranscriptFile => ({ path, unit: "session", session: stem(path) });
const subagentFiles = (sessionDir: string): TranscriptFile[] =>
  jsonlIn(join(sessionDir, "subagents")).map((path) => ({
    path,
    unit: "subagent",
    session: basename(sessionDir),
    agent: stem(path).replace(/^agent-/, ""),
    description: description(path),
  }));

// A directory holds <session>.jsonl and <session>/subagents/*.jsonl.
function inDir(dir: string): TranscriptFile[] {
  const subdirs = readdirSync(dir).map((n) => join(dir, n)).filter(isDir);
  return [...jsonlIn(dir).map(sessionFile), ...subdirs.flatMap(subagentFiles)];
}

function bySession(id: string, root: string): TranscriptFile[] {
  if (!isDir(root)) return [];
  const projects = readdirSync(root).map((n) => join(root, n)).filter(isDir);
  return projects.flatMap((p) => {
    const file = join(p, `${id}.jsonl`);
    return [...(existsSync(file) ? [sessionFile(file)] : []), ...subagentFiles(join(p, id))];
  });
}

// Files of one target are sorted by path, so a session comes before its subagents.
export function locate(targets: string[], root: string): TranscriptFile[] {
  const seen = new Set<string>();
  return targets.flatMap((t) =>
    (isDir(t) ? inDir(t) : bySession(t, root))
      .sort((a, b) => (a.path < b.path ? -1 : a.path > b.path ? 1 : 0))
      .filter((f) => !seen.has(f.path) && !!seen.add(f.path)),
  );
}
