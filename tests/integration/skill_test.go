package integration

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAgentSkillContract(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	skillDir := filepath.Join(root, "skills", "agent-whiteboard")
	skillPath := filepath.Join(skillDir, "SKILL.md")
	content, err := os.ReadFile(skillPath)
	require.NoError(t, err)

	frontmatter, body := splitSkillFrontmatter(t, string(content))
	require.Len(t, frontmatter, 2, "skill frontmatter must contain only name and description")
	require.Equal(t, "agent-whiteboard", frontmatter["name"])
	require.NotEmpty(t, frontmatter["description"], "skill frontmatter must describe when to use the skill")
	require.True(t, strings.HasPrefix(frontmatter["description"], "Use when "),
		"skill description must begin with a third-person triggering condition")
	require.NotEmpty(t, strings.TrimSpace(body), "skill body must not be empty")

	for _, reference := range localMarkdownLinks(body) {
		target := filepath.Clean(filepath.Join(skillDir, filepath.FromSlash(reference)))
		rel, relErr := filepath.Rel(skillDir, target)
		require.NoError(t, relErr)
		require.False(t, rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)),
			"skill reference escapes the skill directory: %s", reference)
		info, statErr := os.Stat(target)
		require.NoError(t, statErr, "skill reference does not exist: %s", reference)
		require.False(t, info.IsDir(), "skill reference must be a file: %s", reference)
	}

	for _, command := range []string{
		"agent-whiteboard serve",
		"agent-whiteboard create markdown",
		"agent-whiteboard create html",
		"agent-whiteboard update markdown",
		"agent-whiteboard update html",
		"agent-whiteboard delete markdown",
		"agent-whiteboard delete html",
		"agent-whiteboard image upload",
		"agent-whiteboard image update",
		"agent-whiteboard image delete",
	} {
		require.Contains(t, body, command, "SKILL.md must contain approved command %q", command)
	}

	lower := strings.ToLower(body)
	for _, prohibition := range []string{
		"credentials",
		"tokens",
		"personal",
		"sensitive",
		"private source",
		"10 mib",
		"25 mib",
		"100 mib",
		"browser",
		"noindex",
		"same-origin",
		"no separate edit token",
	} {
		require.Contains(t, lower, prohibition, "SKILL.md must state required contract %q", prohibition)
	}

	for _, renderingRequirement := range []string{
		"ui/ux",
		"frontend-design",
		"headless-browser",
		"browser-automation",
		"markdown, mermaid, or standalone html",
		"do not return the final url until verification succeeds",
	} {
		require.Contains(t, lower, renderingRequirement,
			"SKILL.md must state rendering requirement %q", renderingRequirement)
	}

	securityPath := filepath.Join(skillDir, "references", "security.md")
	securityContent, err := os.ReadFile(securityPath)
	require.NoError(t, err)
	require.NotContains(t, strings.ToLower(string(securityContent)),
		"return the generated url without opening a browser or fetching the published active document",
		"security guidance must allow the skill's rendering verification workflow")
}

func splitSkillFrontmatter(t *testing.T, content string) (map[string]string, string) {
	t.Helper()
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	require.True(t, strings.HasPrefix(normalized, "---\n"), "SKILL.md must begin with YAML frontmatter")
	parts := strings.SplitN(strings.TrimPrefix(normalized, "---\n"), "\n---\n", 2)
	require.Len(t, parts, 2, "SKILL.md YAML frontmatter must have a closing delimiter")

	frontmatter := make(map[string]string)
	for _, line := range strings.Split(parts[0], "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, ":")
		require.True(t, found, "invalid YAML frontmatter line %q", line)
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		require.NotEmpty(t, key, "frontmatter key must not be empty")
		require.NotEmpty(t, value, "frontmatter value for %q must not be empty", key)
		require.NotContains(t, frontmatter, key, "duplicate frontmatter key %q", key)
		frontmatter[key] = strings.Trim(value, "\"'")
	}
	return frontmatter, parts[1]
}

func localMarkdownLinks(markdown string) []string {
	links := regexp.MustCompile(`\[[^\]]+\]\(([^)]+)\)`).FindAllStringSubmatch(markdown, -1)
	result := make([]string, 0, len(links))
	for _, match := range links {
		target := strings.TrimSpace(strings.SplitN(match[1], "#", 2)[0])
		if target == "" || strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
			continue
		}
		result = append(result, target)
	}
	return result
}
