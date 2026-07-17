import { expect, test as base } from "@playwright/test";
import { spawn } from "node:child_process";
import { promises as fs } from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

const projectRoot = fileURLToPath(new URL("../../", import.meta.url));
const processTimeout = 10_000;
const pollInterval = 20;

function isolatedEnvironment(home) {
  const environment = {};
  for (const [key, value] of Object.entries(process.env)) {
    const normalized = key.toUpperCase();
    if (["HOME", "USERPROFILE", "XDG_CONFIG_HOME"].includes(normalized)) continue;
    if (normalized.startsWith("AGENT_WHITEBOARD_")) continue;
    environment[key] = value;
  }
  return {
    ...environment,
    HOME: home,
    USERPROFILE: home,
    XDG_CONFIG_HOME: path.join(home, ".config"),
  };
}

function runProcess(command, args, { cwd = projectRoot, env = process.env, timeout = 60_000 } = {}) {
  return new Promise((resolve, reject) => {
    const child = spawn(command, args, { cwd, env, stdio: ["ignore", "pipe", "pipe"] });
    let stdout = "";
    let stderr = "";
    let timedOut = false;
    let killWaitTimer;
    const timer = setTimeout(() => {
      timedOut = true;
      child.kill("SIGKILL");
      killWaitTimer = setTimeout(() => {
        reject(new Error(`timed-out process did not exit after SIGKILL: ${command} ${args.join(" ")}`));
      }, 5_000);
    }, timeout);
    child.stdout.setEncoding("utf8");
    child.stderr.setEncoding("utf8");
    child.stdout.on("data", (chunk) => {
      stdout += chunk;
    });
    child.stderr.on("data", (chunk) => {
      stderr += chunk;
    });
    child.once("error", (error) => {
      clearTimeout(timer);
      clearTimeout(killWaitTimer);
      reject(error);
    });
    child.once("exit", (code, signal) => {
      clearTimeout(timer);
      clearTimeout(killWaitTimer);
      if (timedOut) {
        reject(new Error(`process timed out: ${command} ${args.join(" ")}\nstdout:\n${stdout}\nstderr:\n${stderr}`));
        return;
      }
      if (code === 0) {
        resolve({ stdout, stderr });
        return;
      }
      reject(new Error(`process failed (${code ?? signal}): ${command} ${args.join(" ")}\nstdout:\n${stdout}\nstderr:\n${stderr}`));
    });
  });
}

function startServer(binary, storage, env) {
  const child = spawn(
    binary,
    ["serve", "--host", "127.0.0.1", "--port", "0", "--storage", storage, "--log-mode", "json"],
    { cwd: projectRoot, env, stdio: ["ignore", "pipe", "pipe"] },
  );
  child.stdout.setEncoding("utf8");
  child.stderr.setEncoding("utf8");

  let stdout = "";
  let stderr = "";
  let pending = "";
  let settled = false;
  const listening = new Promise((resolve, reject) => {
    const timer = setTimeout(() => {
      if (settled) return;
      settled = true;
      reject(new Error(`server listening log timed out\nstdout:\n${stdout}\nstderr:\n${stderr}`));
    }, processTimeout);
    const fail = (error) => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      reject(error);
    };
    child.once("error", fail);
    child.once("exit", (code, signal) => {
      fail(new Error(`server exited before listening (${code ?? signal})\nstdout:\n${stdout}\nstderr:\n${stderr}`));
    });
    child.stderr.on("data", (chunk) => {
      stderr += chunk;
      pending += chunk;
      for (;;) {
        const newline = pending.indexOf("\n");
        if (newline < 0) break;
        const line = pending.slice(0, newline).trim();
        pending = pending.slice(newline + 1);
        try {
          const entry = JSON.parse(line);
          if (entry.msg !== "server listening") continue;
          const parsed = new URL(entry.url);
          if (parsed.protocol !== "http:" || !entry.address) throw new Error("invalid listening log");
          if (!settled) {
            settled = true;
            clearTimeout(timer);
            resolve({ address: entry.address, url: parsed.origin });
          }
        } catch (error) {
          if (line.includes('"msg":"server listening"')) fail(error);
        }
      }
    });
  });
  child.stdout.on("data", (chunk) => {
    stdout += chunk;
  });
  return { child, listening, output: () => ({ stdout, stderr }) };
}

async function waitForReady(url, child, output) {
  const deadline = Date.now() + processTimeout;
  let lastError;
  while (Date.now() < deadline) {
    if (child.exitCode !== null || child.signalCode !== null) {
      const captured = output();
      throw new Error(`server exited before readiness\nstdout:\n${captured.stdout}\nstderr:\n${captured.stderr}`);
    }
    try {
      const response = await fetch(`${url}/readyz`, { signal: AbortSignal.timeout(500) });
      await response.body?.cancel();
      if (response.status === 200) return;
    } catch (error) {
      lastError = error;
    }
    await new Promise((resolve) => setTimeout(resolve, pollInterval));
  }
  const captured = output();
  throw new Error(`server readiness timed out: ${lastError}\nstdout:\n${captured.stdout}\nstderr:\n${captured.stderr}`);
}

async function waitForExit(child, timeout) {
  if (child.exitCode !== null || child.signalCode !== null) return true;
  return Promise.race([
    new Promise((resolve) => child.once("exit", () => resolve(true))),
    new Promise((resolve) => setTimeout(() => resolve(false), timeout)),
  ]);
}

async function stopServer(child) {
  if (!child || child.exitCode !== null || child.signalCode !== null) return;
  child.kill("SIGTERM");
  if (await waitForExit(child, 5_000)) return;
  child.kill("SIGKILL");
  if (!(await waitForExit(child, 5_000))) throw new Error("server process did not exit after SIGKILL");
}

export const test = base.extend({
  server: [
    async ({}, use) => {
      const root = await fs.mkdtemp(path.join(os.tmpdir(), "agent-whiteboard-browser-"));
      const binary = path.join(root, process.platform === "win32" ? "agent-whiteboard.exe" : "agent-whiteboard");
      const storage = path.join(root, "storage");
      const home = path.join(root, "home");
      const env = isolatedEnvironment(home);
      let running;
      try {
        await fs.mkdir(storage, { recursive: true });
        await fs.mkdir(home, { recursive: true });
        await runProcess("go", ["build", "-trimpath", "-o", binary, "./cmd/agent-whiteboard"]);
        running = startServer(binary, storage, env);
        const listening = await running.listening;
        await waitForReady(listening.url, running.child, running.output);
        await use({ ...listening, binary, child: running.child, env, root, storage });
      } finally {
        try {
          await stopServer(running?.child);
        } finally {
          await fs.rm(root, { recursive: true, force: true });
        }
      }
    },
    { scope: "worker" },
  ],

  publish: async ({ server }, use) => {
    let sequence = 0;
    await use(async (markdown) => {
      const fixturePath = path.join(server.root, `fixture-${sequence++}.md`);
      await fs.writeFile(fixturePath, markdown, { mode: 0o600 });
      const { stdout, stderr } = await runProcess(
        server.binary,
        ["--server", server.url, "--json", "create", "markdown", "--expires-in", "0", fixturePath],
        { env: server.env, timeout: processTimeout },
      );
      if (stderr !== "") throw new Error(`CLI wrote unexpected stderr: ${stderr}`);
      const envelope = JSON.parse(stdout);
      if (envelope.schema_version !== 1 || typeof envelope.resource?.url !== "string") {
        throw new Error(`invalid CLI JSON: ${stdout}`);
      }
      return envelope.resource;
    });
  },

  networkRequests: [
    async ({ page, server }, use) => {
      const all = [];
      const external = [];
      await page.route("**/*", async (route) => {
        const requestURL = route.request().url();
        all.push(requestURL);
        if (new URL(requestURL).origin !== new URL(server.url).origin) {
          external.push(requestURL);
          await route.abort("blockedbyclient");
          return;
        }
        await route.continue();
      });
      await use({ all, external });
      expect(external, "external browser requests").toEqual([]);
    },
    { auto: true },
  ],
});

export { expect };
