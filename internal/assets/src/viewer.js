import createDOMPurify from "dompurify";
import hljs from "highlight.js/lib/common";
import MarkdownIt from "markdown-it";
import mermaid from "mermaid";

export const DEFAULT_TITLE = "Untitled whiteboard";
export const THEME_STORAGE_KEY = "agent-whiteboard-theme";

const ALLOWED_THEMES = new Set(["light", "dark", "system"]);
const MERMAID_SECURE_KEYS = [
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
];

export function normalizeTheme(value) {
  return ALLOWED_THEMES.has(value) ? value : "system";
}

export function resolveTheme(theme, mediaQuery) {
  if (normalizeTheme(theme) !== "system") return theme;
  return mediaQuery?.matches ? "dark" : "light";
}

function installTaskListRule(markdown) {
  markdown.core.ruler.after("inline", "task-list-items", (state) => {
    for (let index = 2; index < state.tokens.length; index += 1) {
      const inline = state.tokens[index];
      const listItem = state.tokens[index - 2];
      if (inline.type !== "inline" || listItem.type !== "list_item_open" || !inline.children?.length) continue;

      const first = inline.children[0];
      if (first.type !== "text") continue;
      const match = /^\[([ xX])\]\s+/.exec(first.content);
      if (!match) continue;

      first.content = first.content.slice(match[0].length);
      const checkbox = new state.Token("task_checkbox", "input", 0);
      checkbox.meta = { checked: match[1].toLowerCase() === "x" };
      inline.children.unshift(checkbox);
      listItem.attrJoin("class", "task-list-item");
    }
  });

  markdown.renderer.rules.task_checkbox = (tokens, index) =>
    `<input class="task-list-item-checkbox" type="checkbox" disabled${tokens[index].meta.checked ? " checked" : ""}> `;
}

function createMarkdownRenderer(diagramSources) {
  const markdown = new MarkdownIt({
    html: false,
    linkify: true,
  });
  const defaultFence = markdown.renderer.rules.fence.bind(markdown.renderer.rules);

  installTaskListRule(markdown);
  markdown.renderer.rules.fence = (tokens, index, options, environment, renderer) => {
    const token = tokens[index];
    const language = token.info.trim().split(/\s+/u, 1)[0].toLowerCase();
    if (language !== "mermaid") {
      return defaultFence(tokens, index, options, environment, renderer);
    }

    const sourceIndex = diagramSources.push(token.content) - 1;
    return `<div class="mermaid-placeholder" data-index="${sourceIndex}"></div>`;
  };

  return markdown;
}

function purifierFor(doc) {
  return createDOMPurify(doc.defaultView ?? window);
}

function renderMarkdown(source, doc) {
  const diagramSources = [];
  const markdown = createMarkdownRenderer(diagramSources);
  const rendered = markdown.render(source);
  const sanitized = purifierFor(doc).sanitize(rendered);
  return { diagramSources, html: sanitized };
}

function highlightCode(container) {
  for (const code of container.querySelectorAll("pre code")) {
    hljs.highlightElement(code);
  }
}

function setDocumentTitle(container, doc) {
  const firstHeading = container.querySelector("h1");
  const title = firstHeading?.textContent?.trim();
  doc.title = title || DEFAULT_TITLE;
}

function replaceWithDiagramError(placeholder, doc) {
  const error = doc.createElement("div");
  error.className = "mermaid-error";
  error.setAttribute("role", "img");
  error.setAttribute("aria-label", "Diagram rendering failed");
  error.textContent = "Unable to render diagram";
  placeholder.replaceChildren(error);
}

async function renderDiagrams({ container, diagramSources, doc, resolvedTheme, generation, isCurrent }) {
  mermaid.initialize({
    startOnLoad: false,
    securityLevel: "strict",
    suppressErrorRendering: true,
    maxTextSize: 50_000,
    maxEdges: 500,
    theme: resolvedTheme,
    htmlLabels: false,
    secure: MERMAID_SECURE_KEYS,
  });

  const purifier = purifierFor(doc);
  const placeholders = [...container.querySelectorAll(".mermaid-placeholder")];
  for (const placeholder of placeholders) {
    const index = Number.parseInt(placeholder.dataset.index ?? "", 10);
    const source = diagramSources[index];
    if (!Number.isSafeInteger(index) || typeof source !== "string") {
      replaceWithDiagramError(placeholder, doc);
      continue;
    }

    try {
      const id = `agent-whiteboard-mermaid-${generation}-${index}`;
      const result = await mermaid.render(id, source);
      if (!isCurrent()) return;
      const sanitizedSVG = purifier.sanitize(result.svg, {
        USE_PROFILES: { svg: true, svgFilters: true },
      });
      placeholder.innerHTML = sanitizedSVG;
    } catch {
      if (isCurrent()) replaceWithDiagramError(placeholder, doc);
    }
  }
}

function browserStorage(doc) {
  try {
    return doc.defaultView?.localStorage;
  } catch {
    return undefined;
  }
}

function browserMediaQuery(doc) {
  return typeof doc.defaultView?.matchMedia === "function"
    ? doc.defaultView.matchMedia("(prefers-color-scheme: dark)")
    : { matches: false, addEventListener() {}, removeEventListener() {} };
}

function readTheme(storage) {
  try {
    return normalizeTheme(storage?.getItem(THEME_STORAGE_KEY));
  } catch {
    return "system";
  }
}

function persistTheme(storage, theme) {
  try {
    storage?.setItem(THEME_STORAGE_KEY, theme);
  } catch {
    // Rendering remains available when browser storage is disabled.
  }
}

export async function renderWhiteboard(
  source,
  {
    container,
    doc = document,
    storage = browserStorage(doc),
    mediaQuery = browserMediaQuery(doc),
  } = {},
) {
  if (typeof source !== "string") throw new TypeError("whiteboard source must be a string");
  if (!container) throw new TypeError("viewer container is required");

  const { diagramSources, html } = renderMarkdown(source, doc);
  container.innerHTML = html;
  highlightCode(container);
  setDocumentTitle(container, doc);

  let theme = readTheme(storage);
  let generation = 0;
  let pendingRender = Promise.resolve();
  let subscribed = false;

  const onSystemThemeChange = () => {
    if (theme === "system") queueDiagramRender();
  };

  function syncSystemSubscription() {
    if (theme === "system" && !subscribed) {
      mediaQuery.addEventListener?.("change", onSystemThemeChange);
      subscribed = true;
    } else if (theme !== "system" && subscribed) {
      mediaQuery.removeEventListener?.("change", onSystemThemeChange);
      subscribed = false;
    }
  }

  function queueDiagramRender() {
    const selectedTheme = theme;
    const resolvedTheme = resolveTheme(selectedTheme, mediaQuery);
    const renderGeneration = ++generation;
    doc.documentElement.dataset.theme = resolvedTheme;
    doc.documentElement.style.colorScheme = resolvedTheme;
    pendingRender = pendingRender.then(() =>
      renderDiagrams({
        container,
        diagramSources,
        doc,
        resolvedTheme,
        generation: renderGeneration,
        isCurrent: () => renderGeneration === generation,
      }),
    );
    return pendingRender;
  }

  const controller = {
    diagramSources: [...diagramSources],
    get theme() {
      return theme;
    },
    async setTheme(value) {
      theme = normalizeTheme(value);
      persistTheme(storage, theme);
      syncSystemSubscription();
      await queueDiagramRender();
    },
    settled() {
      return pendingRender;
    },
    destroy() {
      if (subscribed) mediaQuery.removeEventListener?.("change", onSystemThemeChange);
      subscribed = false;
    },
  };

  persistTheme(storage, theme);
  syncSystemSubscription();
  await queueDiagramRender();
  return controller;
}

function viewerContainer(doc) {
  const existing = doc.querySelector("#agent-whiteboard-content");
  if (existing) return existing;
  const container = doc.createElement("main");
  container.id = "agent-whiteboard-content";
  doc.body.append(container);
  return container;
}

export async function bootViewer(doc = document) {
  const sourceElement = doc.querySelector("#agent-whiteboard-source");
  if (!sourceElement) return undefined;
  const payload = JSON.parse(sourceElement.textContent || "null");
  if (payload === null || typeof payload !== "object" || Array.isArray(payload) || typeof payload.markdown !== "string") {
    throw new TypeError("invalid whiteboard source payload");
  }
  return renderWhiteboard(payload.markdown, { container: viewerContainer(doc), doc });
}

function startBrowserEntry() {
  void bootViewer().catch(() => {
    const container = viewerContainer(document);
    container.replaceChildren();
    const error = document.createElement("p");
    error.className = "viewer-error";
    error.textContent = "Unable to render whiteboard";
    container.append(error);
    document.title = DEFAULT_TITLE;
  });
}

if (typeof document !== "undefined") {
  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", startBrowserEntry, { once: true });
  } else if (document.querySelector("#agent-whiteboard-source")) {
    startBrowserEntry();
  }
}
