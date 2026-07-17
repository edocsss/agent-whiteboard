import { beforeEach, describe, expect, test, vi } from "vitest";

const mermaidMocks = vi.hoisted(() => ({
  initialize: vi.fn(),
  render: vi.fn(),
}));

vi.mock("mermaid", () => ({
  default: mermaidMocks,
}));

import {
  DEFAULT_TITLE,
  THEME_STORAGE_KEY,
  bootViewer,
  normalizeTheme,
  renderWhiteboard,
} from "./viewer.js";

function makeMediaQuery(dark = false) {
  let matches = dark;
  const listeners = new Set();

  return {
    get matches() {
      return matches;
    },
    addEventListener: vi.fn((event, listener) => {
      if (event === "change") listeners.add(listener);
    }),
    removeEventListener: vi.fn((event, listener) => {
      if (event === "change") listeners.delete(listener);
    }),
    change(next) {
      matches = next;
      for (const listener of listeners) listener({ matches });
    },
  };
}

function setupDOM() {
  document.head.innerHTML = "";
  document.body.innerHTML = '<main id="viewer"></main>';
  document.documentElement.removeAttribute("data-theme");
  localStorage.clear();
  return document.querySelector("#viewer");
}

beforeEach(() => {
  vi.clearAllMocks();
  setupDOM();
  mermaidMocks.render.mockImplementation(async (id, source) => ({
    svg: `<svg xmlns="http://www.w3.org/2000/svg" data-id="${id}"><text>${source}</text></svg>`,
  }));
});

describe("Markdown rendering", () => {
  test("boots from the viewer shell JSON object contract", async () => {
    const source = "# Shell title\n\nRendered from the shell.";
    const sourceElement = document.createElement("script");
    sourceElement.type = "application/json";
    sourceElement.id = "agent-whiteboard-source";
    sourceElement.textContent = JSON.stringify({ markdown: source });
    document.body.replaceChildren(sourceElement);

    const viewer = await bootViewer(document);

    const container = document.querySelector("#agent-whiteboard-content");
    expect(container).not.toBeNull();
    expect(container.querySelector("h1")?.textContent).toBe("Shell title");
    expect(container.textContent).toContain("Rendered from the shell.");
    expect(document.title).toBe("Shell title");
    viewer.destroy();
  });

  test("renders supported Markdown and task lists with real markdown-it", async () => {
    const container = document.querySelector("#viewer");
    const source = [
      "# Board",
      "",
      "> quoted",
      "",
      "| Name | Value |",
      "| --- | --- |",
      "| A | B |",
      "",
      "- [ ] pending",
      "- [x] complete",
      "",
      "[safe](https://example.com)",
      "",
      "```js",
      "const answer = 42;",
      "```",
    ].join("\n");

    await renderWhiteboard(source, { container });

    expect(container.querySelector("h1")?.textContent).toBe("Board");
    expect(container.querySelector("blockquote")?.textContent).toContain("quoted");
    expect(container.querySelector("table tbody td")?.textContent).toBe("A");
    expect(container.querySelector('a[href="https://example.com"]')?.textContent).toBe("safe");
    expect(container.querySelector("pre code")?.textContent).toContain("const answer = 42;");
    expect(container.querySelector("pre code")?.classList.contains("hljs")).toBe(true);
    expect([...container.querySelectorAll('input[type="checkbox"]')]).toHaveLength(2);
    expect([...container.querySelectorAll('input[type="checkbox"]')].every((box) => box.disabled)).toBe(true);
    expect(container.querySelectorAll('input[type="checkbox"]')[1].checked).toBe(true);
  });

  test("does not allow raw Markdown HTML or javascript links to survive", async () => {
    const container = document.querySelector("#viewer");

    await renderWhiteboard(
      '<img src="x" onerror="globalThis.pwned=true">\n\n[unsafe](javascript:alert(1))',
      { container },
    );

    expect(container.querySelector("img")).toBeNull();
    expect(container.querySelector('a[href^="javascript:"]')).toBeNull();
  });

  test("uses the first rendered H1 as the title", async () => {
    const container = document.querySelector("#viewer");

    await renderWhiteboard("## Before\n\n# First title\n\n# Second title", { container });

    expect(document.title).toBe("First title");
  });

  test("uses the fallback title when there is no H1", async () => {
    const container = document.querySelector("#viewer");

    await renderWhiteboard("## Board", { container });

    expect(document.title).toBe(DEFAULT_TITLE);
  });

  test("provides a theme menu that applies and persists Light", async () => {
    const container = document.querySelector("#viewer");

    const viewer = await renderWhiteboard("# Board", { container });

    const trigger = container.querySelector("[data-theme-control]");
    const menu = container.querySelector("[data-theme-menu]");
    expect(trigger?.textContent).toBe("Theme: System");
    expect(menu?.hidden).toBe(true);
    expect([...container.querySelectorAll("[data-theme-option]")].map((option) => option.textContent)).toEqual([
      "System",
      "Light",
      "Dark",
    ]);

    trigger.click();
    expect(trigger.getAttribute("aria-expanded")).toBe("true");
    container.querySelector('[data-theme-option="light"]').click();
    await viewer.settled();

    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe("light");
    expect(document.documentElement.dataset.theme).toBe("light");
    expect(trigger.textContent).toBe("Theme: Light");
    expect(menu.hidden).toBe(true);
  });
});

describe("themes", () => {
  test.each([
    ["light", "light"],
    ["dark", "dark"],
    ["system", "system"],
    ["unknown", "system"],
    [null, "system"],
    [undefined, "system"],
  ])("normalizes %j to %s", (input, expected) => {
    expect(normalizeTheme(input)).toBe(expected);
  });

  test("persists only the allowed theme key and follows live system changes", async () => {
    const container = document.querySelector("#viewer");
    const mediaQuery = makeMediaQuery(false);
    localStorage.setItem(THEME_STORAGE_KEY, "not-a-theme");

    const viewer = await renderWhiteboard("```mermaid\ngraph TD; A-->B\n```", {
      container,
      mediaQuery,
      storage: localStorage,
    });

    expect(viewer.theme).toBe("system");
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe("system");
    expect(document.documentElement.dataset.theme).toBe("light");
    expect(mermaidMocks.render).toHaveBeenCalledTimes(1);

    mediaQuery.change(true);
    await viewer.settled();

    expect(document.documentElement.dataset.theme).toBe("dark");
    expect(mermaidMocks.render).toHaveBeenCalledTimes(2);

    await viewer.setTheme("light");
    mediaQuery.change(false);
    await viewer.settled();

    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe("light");
    expect(document.documentElement.dataset.theme).toBe("light");
    expect(mermaidMocks.render).toHaveBeenCalledTimes(3);
    expect(Object.keys(localStorage)).toEqual([THEME_STORAGE_KEY]);
  });

  test("restores stored theme and closes the menu with Escape or outside click", async () => {
    const container = document.querySelector("#viewer");
    localStorage.setItem(THEME_STORAGE_KEY, "dark");

    const viewer = await renderWhiteboard("# Board", { container });
    const trigger = container.querySelector("[data-theme-control]");
    const menu = container.querySelector("[data-theme-menu]");

    expect(trigger.textContent).toBe("Theme: Dark");
    trigger.click();
    document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
    expect(menu.hidden).toBe(true);
    expect(document.activeElement).toBe(trigger);

    trigger.click();
    document.dispatchEvent(new Event("pointerdown", { bubbles: true }));
    expect(menu.hidden).toBe(true);

    viewer.destroy();
    expect(container.querySelector("[data-theme-control]")).toBeNull();
  });
});

describe("Mermaid rendering", () => {
  test("emits indexed placeholders while retaining source outside HTML attributes", async () => {
    const container = document.querySelector("#viewer");
    const first = 'flowchart LR\nA["quoted"] --> B';
    const second = "sequenceDiagram\nA->>B: hello";

    const viewer = await renderWhiteboard(
      `\`\`\`mermaid\n${first}\n\`\`\`\n\n\`\`\`mermaid\n${second}\n\`\`\``,
      { container },
    );

    const placeholders = [...container.querySelectorAll(".mermaid-placeholder")];
    expect(placeholders.map((node) => node.dataset.index)).toEqual(["0", "1"]);
    expect(placeholders.every((node) => node.getAttribute("data-source") === null)).toBe(true);
    expect(viewer.diagramSources).toEqual([`${first}\n`, `${second}\n`]);
  });

  test("isolates an invalid diagram to its own error block", async () => {
    const container = document.querySelector("#viewer");
    mermaidMocks.render.mockImplementation(async (id, source) => {
      if (source.includes("invalid")) throw new Error("parse details must not leak");
      return { svg: `<svg xmlns="http://www.w3.org/2000/svg"><text>${id}</text></svg>` };
    });

    await renderWhiteboard(
      "```mermaid\ngraph TD; A-->B\n```\n\n```mermaid\ninvalid\n```\n\n```mermaid\ngraph LR; C-->D\n```",
      { container },
    );

    const placeholders = [...container.querySelectorAll(".mermaid-placeholder")];
    expect(placeholders[0].querySelector("svg")).not.toBeNull();
    expect(placeholders[1].querySelector(".mermaid-error")?.textContent).toBe("Unable to render diagram");
    expect(placeholders[1].textContent).not.toContain("parse details");
    expect(placeholders[2].querySelector("svg")).not.toBeNull();
  });

  test("re-renders every diagram from retained source after a theme change", async () => {
    const container = document.querySelector("#viewer");
    const mediaQuery = makeMediaQuery(false);
    const viewer = await renderWhiteboard(
      "```mermaid\ngraph TD; A-->B\n```\n\n```mermaid\ngraph LR; C-->D\n```",
      { container, mediaQuery },
    );

    expect(mermaidMocks.render.mock.calls.map((call) => call[1])).toEqual([
      "graph TD; A-->B\n",
      "graph LR; C-->D\n",
    ]);

    await viewer.setTheme("dark");

    expect(mermaidMocks.render.mock.calls.map((call) => call[1])).toEqual([
      "graph TD; A-->B\n",
      "graph LR; C-->D\n",
      "graph TD; A-->B\n",
      "graph LR; C-->D\n",
    ]);
    expect(mermaidMocks.initialize).toHaveBeenLastCalledWith(
      expect.objectContaining({
        startOnLoad: false,
        securityLevel: "strict",
        theme: "dark",
        htmlLabels: false,
        secure: expect.arrayContaining([
          "secure",
          "securityLevel",
          "startOnLoad",
          "maxTextSize",
          "suppressErrorRendering",
          "maxEdges",
          "theme",
          "htmlLabels",
          "themeCSS",
          "themeVariables",
        ]),
      }),
    );
  });

  test("sanitizes rendered SVG before insertion", async () => {
    const container = document.querySelector("#viewer");
    mermaidMocks.render.mockResolvedValue({
      svg: '<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script><a href="javascript:alert(1)"><text>safe</text></a></svg>',
    });

    await renderWhiteboard("```mermaid\ngraph TD; A-->B\n```", { container });

    expect(container.querySelector("svg")).not.toBeNull();
    expect(container.querySelector("svg script")).toBeNull();
    expect(container.querySelector('svg a[href^="javascript:"]')).toBeNull();
  });
});
