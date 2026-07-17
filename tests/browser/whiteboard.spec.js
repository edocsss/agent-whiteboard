import { expect, test } from "./fixture.js";

const completeMarkdown = [
  "# Browser Whiteboard",
  "",
  "## Rendered structures",
  "",
  "> quoted content",
  "",
  "| Name | Value |",
  "| --- | --- |",
  "| Alpha | Beta |",
  "",
  "- [ ] pending task",
  "- [x] completed task",
  "",
  "[safe link](https://example.com/path)",
  "",
  "[unsafe link](javascript:window.__unsafeLinkExecuted=true)",
  "",
  "```go",
  "package main",
  "func main() {}",
  "```",
  "",
  "```mermaid",
  "flowchart LR",
  "  A[First] --> B[Second]",
  "```",
  "",
  "```mermaid",
  "sequenceDiagram",
  "  Alice->>Bob: Hello",
  "```",
].join("\n");

async function waitForDiagrams(page, count) {
  await expect(page.locator(".mermaid-placeholder svg")).toHaveCount(count);
}

test("renders the complete Markdown contract without external requests", async ({
  page,
  publish,
  networkRequests,
  server,
}) => {
  const resource = await publish(completeMarkdown);
  await page.goto(resource.url);

  await expect(page.locator("#agent-whiteboard-content h1").first()).toHaveText("Browser Whiteboard");
  await expect(page.locator("blockquote")).toContainText("quoted content");
  await expect(page.locator("table tbody tr")).toHaveCount(1);
  await expect(page.locator("table tbody td").first()).toHaveText("Alpha");
  const tasks = page.locator('input.task-list-item-checkbox[type="checkbox"]');
  await expect(tasks).toHaveCount(2);
  await expect(tasks.nth(0)).toBeDisabled();
  await expect(tasks.nth(1)).toBeDisabled();
  await expect(tasks.nth(1)).toBeChecked();
  await expect(page.locator("pre code.language-go.hljs")).toContainText("package main");
  await expect(page.locator('a[href="https://example.com/path"]')).toHaveText("safe link");
  await expect(page.locator('a[href^="javascript:"]')).toHaveCount(0);

  await waitForDiagrams(page, 2);
  await expect(page.locator(".mermaid-placeholder").nth(0)).toContainText("First");
  await expect(page.locator(".mermaid-placeholder").nth(0)).toContainText("Second");
  for (const diagram of await page.locator(".mermaid-placeholder").all()) {
    await expect(diagram.locator("svg")).toHaveCount(1);
    await expect(diagram.locator("script")).toHaveCount(0);
    expect(await diagram.locator("[onload], [onclick], [onerror]").count()).toBe(0);
  }

  await expect(page).toHaveTitle("Browser Whiteboard");
  await expect(page.locator("#agent-whiteboard-source")).toBeHidden();
  const visibleText = await page.locator("body").innerText();
  expect(visibleText).not.toContain("| Name | Value |");
  expect(visibleText).not.toContain("```mermaid");
  expect(visibleText).not.toContain("flowchart LR");

  expect(networkRequests.all.length).toBeGreaterThan(0);
  expect(networkRequests.external).toEqual([]);
  expect(networkRequests.all.every((requestURL) => new URL(requestURL).origin === new URL(server.url).origin)).toBe(true);
  expect(networkRequests.all.some((requestURL) => /cdn|jsdelivr|unpkg/iu.test(requestURL))).toBe(false);
});

test("persists light dark and system themes and re-renders Mermaid", async ({ page, publish }) => {
  const resource = await publish("# Themes\n\n```mermaid\nflowchart LR\n  A --> B\n```\n");
  await page.emulateMedia({ colorScheme: "light" });
  await page.goto(resource.url);

  const setThemeAndReload = async (theme) => {
    await page.evaluate((value) => localStorage.setItem("agent-whiteboard-theme", value), theme);
    await page.reload();
    await waitForDiagrams(page, 1);
    await expect.poll(() => page.evaluate(() => localStorage.getItem("agent-whiteboard-theme"))).toBe(theme);
    return page.locator(".mermaid-placeholder svg").evaluate((node) => node.outerHTML);
  };

  const lightSVG = await setThemeAndReload("light");
  await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
  const darkSVG = await setThemeAndReload("dark");
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  expect(darkSVG).not.toBe(lightSVG);

  const systemLightSVG = await setThemeAndReload("system");
  await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
  expect(systemLightSVG).not.toBe(darkSVG);
  await page.reload();
  await waitForDiagrams(page, 1);
  await expect.poll(() => page.evaluate(() => localStorage.getItem("agent-whiteboard-theme"))).toBe("system");

  const beforeSystemChange = await page.locator(".mermaid-placeholder svg").evaluate((node) => node.outerHTML);
  await page.emulateMedia({ colorScheme: "dark" });
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  await expect
    .poll(() => page.locator(".mermaid-placeholder svg").evaluate((node) => node.outerHTML))
    .not.toBe(beforeSystemChange);
});

test("isolates an invalid Mermaid diagram from a valid diagram", async ({ page, publish }) => {
  const resource = await publish(
    "# Isolation\n\n```mermaid\nflowchart LR\n  A --> B\n```\n\n```mermaid\nthis is not valid {\n```\n",
  );
  await page.goto(resource.url);

  const placeholders = page.locator(".mermaid-placeholder");
  await expect(placeholders).toHaveCount(2);
  await expect(placeholders.nth(0).locator("svg")).toHaveCount(1);
  await expect(placeholders.nth(0).locator(".mermaid-error")).toHaveCount(0);
  await expect(placeholders.nth(1).locator(".mermaid-error")).toHaveText("Unable to render diagram");
  await expect(placeholders.nth(1).locator("svg")).toHaveCount(0);
  await expect(page.locator(".mermaid-error")).toHaveCount(1);
  await expect(placeholders.nth(1)).not.toContainText("parse");
});
