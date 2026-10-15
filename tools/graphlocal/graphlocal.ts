// graphlocal.ts — render a Mermaid diagram in a browser via a local server.
//
//   node graphlocal.ts serve <diagram-file>   # spawn a detached server and print the URL
//   node graphlocal.ts kill <slug>            # stop the server for <slug>
//   node graphlocal.ts url <slug>             # print the URL from the lock file
//
// State lives in /tmp/graphlocal/:
//   <slug>.html   self-contained HTML (Mermaid from CDN)
//   <slug>.lock   JSON {pid, port, url, created}; dead pid is ignored and cleaned
//   <slug>.sock   unix socket; receiving "stop" shuts the server down cleanly
// Invalid input -> message on stderr, exit 1.
import { spawn } from "node:child_process";
import { createServer, type Server } from "node:http";
import { connect, type Socket } from "node:net";
import { existsSync, mkdirSync, readFileSync, unlinkSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { createHash } from "node:crypto";

const DIR = join(tmpdir(), "graphlocal");
const START_TIMEOUT_MS = 5000;

interface Lock {
  pid: number;
  port: number;
  url: string;
  created: string;
}

function readStdin(): string {
  return readFileSync(0, "utf8");
}

function fail(msg: string): never {
  console.error(`error: ${msg}`);
  process.exit(1);
}

function paths(slug: string) {
  return {
    html: join(DIR, `${slug}.html`),
    lock: join(DIR, `${slug}.lock`),
    sock: join(DIR, `${slug}.sock`),
  };
}

function readLock(lock: string): Lock | null {
  if (!existsSync(lock)) return null;
  try {
    const l = JSON.parse(readFileSync(lock, "utf8")) as Lock;
    return Number.isInteger(l.pid) && Number.isInteger(l.port) ? l : null;
  } catch {
    return null;
  }
}

function alive(pid: number): boolean {
  try {
    process.kill(pid, 0);
    return true;
  } catch (e) {
    return (e as NodeJS.ErrnoException).code === "EPERM";
  }
}

function slugFor(source: string, label?: string): string {
  const hash = createHash("sha1").update(source).digest("hex").slice(0, 8);
  const base = (label ?? "").replace(/[^a-zA-Z0-9]+/g, "-").replace(/^-+|-+$/g, "").slice(0, 32);
  return `${base || "graph"}-${hash}`;
}

function shutDown(httpServer: Server, sockServer: Server, sock: string, lock: string): void {
  httpServer.close();
  sockServer.close();
  for (const f of [sock, lock]) {
    try {
      unlinkSync(f);
    } catch {
      /* already gone */
    }
  }
  process.exit(0);
}

// Single worker: one http server (port 0) + one unix socket for control.
function worker(mermaid: string, label: string): void {
  mkdirSync(DIR, { recursive: true });
  const slug = slugFor(mermaid, label);
  const { html, lock, sock } = paths(slug);

  // A previous server for the same slug is killed before starting a new one.
  const prev = readLock(lock);
  if (prev && alive(prev.pid)) {
    try {
      unlinkSync(lock); // so the parent's poll cannot see the stale lock as ours
      process.kill(prev.pid, "SIGTERM");
      waitGone(prev.pid);
    } catch {
      /* raced with exit */
    }
  }

  const page = renderPage(mermaid, label || "Diagram");
  writeFileSync(html, page);

  let current = (): string => page; // re-read from disk on every GET so /edit shows through
  current = (): string => {
    try {
      return readFileSync(html, "utf8");
    } catch {
      return page;
    }
  };

  const httpServer = createServer((req, res) => {
    if (req.method === "GET" && req.url === "/") {
      res.writeHead(200, { "content-type": "text/html; charset=utf-8" });
      res.end(current());
      return;
    }
    if (req.method === "POST" && req.url === "/edit") {
      const chunks: Buffer[] = [];
      req.on("data", (c: Buffer) => chunks.push(c));
      req.on("end", () => {
        try {
          writeFileSync(html, Buffer.concat(chunks).toString("utf8"));
          res.writeHead(200);
          res.end("ok");
        } catch (e) {
          res.writeHead(500);
          res.end(String(e));
        }
      });
      return;
    }
    res.writeHead(404, { "content-type": "text/plain" });
    res.end("not found");
  });

  const sockServer = createServer();
  sockServer.on("connection", (c: Socket) => {
    c.on("end", () => shutDown(httpServer, sockServer, sock, lock));
    c.on("close", () => shutDown(httpServer, sockServer, sock, lock));
  });
  try {
    unlinkSync(sock); // stale socket from a crashed run
  } catch {
    /* fresh */
  }
  sockServer.listen(sock);

  httpServer.listen(0, "127.0.0.1", () => {
    const addr = httpServer.address();
    if (addr === null || typeof addr === "string") fail("no port assigned");
    const url = `http://127.0.0.1:${addr.port}/`;
    writeFileSync(lock, JSON.stringify({ pid: process.pid, port: addr.port, url, created: new Date().toISOString() } satisfies Lock));
  });

  const stop = (): void => shutDown(httpServer, sockServer, sock, lock);
  process.on("SIGINT", stop);
  process.on("SIGTERM", stop);
}

function waitGone(pid: number): void {
  for (let i = 0; i < 20 && alive(pid); i++) {
    const until = Date.now() + 50;
    while (Date.now() < until);
  }
}

function escHtml(s: string): string {
  return s.replace(/&/g, "&").replace(/</g, "<").replace(/>/g, ">").replace(/"/g, '"');
}

function renderPage(mermaid: string, title: string): string {
  const t = escHtml(title);
  return `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>${t}</title>
<style>body{font-family:sans-serif;margin:0;padding:16px}.mermaid{background:#fff}</style>
</head>
<body>
<pre class="mermaid">
${mermaid.trim()}
</pre>
<script type="module">
import mermaid from "https://cdn.jsdelivr.net/npm/mermaid@11/dist/mermaid.esm.min.mjs";
mermaid.initialize({ startOnLoad: true, securityLevel: "loose", maxTextSize: 9000000, maxEdges: 2000 });
</script>
</body>
</html>
`;
}

// Parent: spawn a detached worker (own pgid), wait until the lock appears, print the URL.
function serve(file: string | undefined): void {
  let mermaid: string;
  try {
    mermaid = file ? readFileSync(file, "utf8") : readStdin();
  } catch (e) {
    fail(`cannot read diagram: ${file ?? "(stdin)"} (${(e as Error).message})`);
  }
  if (!mermaid.trim()) fail("diagram text is empty");
  const label = file ? file.replace(/\.[^.]*$/, "").split("/").pop() ?? "" : "";

  const slug = slugFor(mermaid, label);
  const { lock } = paths(slug);
  const prevPid = readLock(lock)?.pid; // a lock from the previous server is not "ours"

  const child = spawn(process.argv0, [process.argv[1], "__worker"], {
    detached: true,
    stdio: "ignore",
    env: { ...process.env, GRAPHLOCAL_MERMAID: mermaid, GRAPHLOCAL_LABEL: label },
  });
  child.unref();

  const until = Date.now() + START_TIMEOUT_MS;
  while (Date.now() < until) {
    const l = readLock(lock);
    if (l?.port && l.pid !== prevPid) {
      console.log(l.url);
      console.log(`stop with: node ${process.argv[1]} kill ${slug}`);
      return;
    }
    const waitUntil = Date.now() + 100;
    while (Date.now() < waitUntil);
  }
  fail(`server did not start within ${START_TIMEOUT_MS}ms (pid ${child.pid})`);
}

function kill(slug: string): void {
  const { lock, sock } = paths(slug);
  const l = readLock(lock);
  if (!l) {
    console.log(`no server for ${slug}`);
    process.exit(0);
  }
  if (!alive(l.pid)) {
    for (const f of [lock, sock]) {
      try {
        unlinkSync(f);
      } catch {
        /* gone */
      }
    }
    console.log(`stale lock removed (pid ${l.pid} is gone)`);
    process.exit(0);
  }
  // Prefer the socket for a clean shutdown; fall back to SIGTERM.
  if (existsSync(sock)) {
    const c = connect(sock);
    c.on("connect", () => {
      c.end("stop");
      setTimeout(() => process.exit(0), 500).unref();
    });
    c.on("error", () => {
      process.kill(l.pid, "SIGTERM");
      process.exit(0);
    });
    return;
  }
  process.kill(l.pid, "SIGTERM");
  console.log(`killed pid ${l.pid} (no socket)`);
}

function main(): void {
  const [cmd, arg] = process.argv.slice(2);
  if (process.env.GRAPHLOCAL_MERMAID !== undefined && process.argv[2] === "__worker") {
    worker(process.env.GRAPHLOCAL_MERMAID, process.env.GRAPHLOCAL_LABEL ?? "");
    return;
  }
  if (cmd === "serve") {
    serve(arg);
  } else if (cmd === "kill") {
    if (!arg || arg.includes("/")) fail("kill requires <slug> (see the lock file name)");
    kill(arg);
  } else if (cmd === "url") {
    if (!arg) fail("url requires <slug>");
    const l = readLock(paths(arg).lock);
    if (!l || !alive(l.pid)) fail(`no running server for ${arg}`);
    console.log(l.url);
  } else {
    fail("usage: graphlocal.ts serve <file> | kill <slug> | url <slug>  (mermaid text: file arg or stdin)");
  }
}

main();
