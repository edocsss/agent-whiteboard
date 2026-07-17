import { expect, test } from "./fixture.js";

test("sanitizes hostile Markdown and preserves ordinary code", async ({ page, publish, networkRequests }) => {
  const source = [
    "# Security",
    "",
    "<script>window.__rawScriptExecuted = true</script>",
    "",
    "<style>body { display: none }</style>",
    "",
    '<img src="x" onerror="window.__eventExecuted = true">',
    "",
    "[unsafe](javascript:window.__javascriptLinkExecuted=true)",
    "",
    '<svg onload="window.__svgExecuted = true"><script>window.__svgScriptExecuted = true</script></svg>',
    "",
    "</script><script>window.__breakoutExecuted = true</script>",
    "",
    "Ordinary code remains: `const safe = true;`",
  ].join("\n");
  const resource = await publish(source);
  const response = await page.goto(resource.url);
  expect(response).not.toBeNull();

  const content = page.locator("#agent-whiteboard-content");
  await expect(content.locator("script, style, img, svg")).toHaveCount(0);
  await expect(content.locator('a[href^="javascript:"]')).toHaveCount(0);
  expect(
    await content.locator("*").evaluateAll((nodes) =>
      nodes.flatMap((node) => [...node.attributes].filter((attribute) => attribute.name.toLowerCase().startsWith("on"))),
    ),
  ).toEqual([]);
  expect(
    await page.evaluate(() => ({
      breakout: globalThis.__breakoutExecuted,
      event: globalThis.__eventExecuted,
      javascript: globalThis.__javascriptLinkExecuted,
      rawScript: globalThis.__rawScriptExecuted,
      svg: globalThis.__svgExecuted,
      svgScript: globalThis.__svgScriptExecuted,
    })),
  ).toEqual({});
  await expect(content.locator("code")).toHaveText("const safe = true;");

  await expect(page.locator('meta[name="robots"]')).toHaveAttribute("content", "noindex, nofollow, noarchive");
  expect(response.headers()["x-robots-tag"]).toBe("noindex, nofollow, noarchive");
  expect(response.headers()["x-content-type-options"]).toBe("nosniff");
  await expect(page.locator('#agent-whiteboard-source[type="application/json"]')).toHaveCount(1);
  await expect(page.locator("script")).toHaveCount(2);
  expect(await page.locator("#agent-whiteboard-source").textContent()).not.toContain("</script>");
  expect(networkRequests.external).toEqual([]);
});
