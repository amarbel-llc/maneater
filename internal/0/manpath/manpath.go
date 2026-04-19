// Package manpath provides system helpers for discovering and extracting
// content from Unix man pages: manpath resolution, page listing, synopsis
// extraction via mandoc+pandoc, and tldr integration.
package manpath

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

var indexedSections = []string{"1", "2", "3", "4", "5", "6", "7", "8"}

// heuristicManDirs are common in-repo locations for man pages, probed in order.
var heuristicManDirs = []string{"man", "doc/man", "share/man"}

// ListManPages walks the given manpath directories and returns all man page
// keys ("page(section)") found under any manN/ subdirectory, deduplicated and
// sorted.
func ListManPages(manpath []string) ([]string, error) {
	seen := make(map[string]bool)
	var pages []string

	for _, dir := range manpath {
		for _, sec := range indexedSections {
			manDir := filepath.Join(dir, "man"+sec)
			entries, err := os.ReadDir(manDir)
			if err != nil {
				continue
			}
			for _, e := range entries {
				if e.IsDir() {
					continue
				}
				name := e.Name()
				name = strings.TrimSuffix(name, ".gz")
				if ext := filepath.Ext(name); ext == "."+sec {
					name = strings.TrimSuffix(name, ext)
				} else {
					continue
				}
				key := name + "(" + sec + ")"
				if name != "" && !seen[key] {
					seen[key] = true
					pages = append(pages, key)
				}
			}
		}
	}

	sort.Strings(pages)
	return pages, nil
}

// ParsePageKey splits "page(section)" into page and section.
// Returns page and empty section if no parens found.
func ParsePageKey(key string) (string, string) {
	if i := strings.LastIndex(key, "("); i >= 0 && strings.HasSuffix(key, ")") {
		return key[:i], key[i+1 : len(key)-1]
	}
	return key, ""
}

// Resolve assembles the effective manpath: heuristic in-repo dirs (unless
// noAuto) first, then configured include paths, then the system manpath.
// include entries may contain $VAR references and relative paths (resolved
// against cwd).
func Resolve(include []string, noAuto bool, cwd string) ([]string, error) {
	systemPaths, err := systemManpath()
	if err != nil {
		return nil, err
	}

	var prepend []string

	if !noAuto {
		for _, rel := range heuristicManDirs {
			candidate := filepath.Join(cwd, rel)
			if info, err := os.Stat(candidate); err == nil && info.IsDir() {
				prepend = append(prepend, candidate)
			}
		}
	}

	for _, inc := range include {
		inc = os.ExpandEnv(inc)
		if !filepath.IsAbs(inc) {
			inc = filepath.Join(cwd, inc)
		}
		prepend = append(prepend, inc)
	}

	return append(prepend, systemPaths...), nil
}

func systemManpath() ([]string, error) {
	// manpath(1) resolves MANPATH, /etc/man.conf, and platform defaults.
	cmd := exec.Command("manpath")
	out, err := cmd.Output()
	if err != nil {
		return []string{"/usr/share/man", "/usr/local/share/man"}, nil
	}
	raw := strings.TrimSpace(string(out))
	if raw == "" {
		return nil, fmt.Errorf("manpath returned empty")
	}
	return strings.Split(raw, ":"), nil
}

// ExtractSynopsis extracts NAME+SYNOPSIS+DESCRIPTION content from a man page,
// truncated to 500 chars. Returns empty string on failure.
func ExtractSynopsis(manpath []string, page string) string {
	name, section := ParsePageKey(page)
	sourcePath, err := LocateSource(manpath, section, name)
	if err != nil {
		return ""
	}

	markdown, err := renderMarkdown(sourcePath)
	if err != nil {
		return ""
	}

	sections := splitSections(markdown)

	var synopsis strings.Builder
	for _, s := range sections {
		upper := strings.ToUpper(strings.TrimSpace(s.Name))
		if upper == "NAME" || upper == "SYNOPSIS" || upper == "DESCRIPTION" {
			synopsis.WriteString(s.Content)
			synopsis.WriteString("\n")
		}
	}

	text := synopsis.String()
	if len(text) > 500 {
		text = text[:500]
	}

	return strings.TrimSpace(text)
}

// ExtractTldr reads the raw tldr markdown for a page and extracts the
// description and example descriptions, truncated to 500 chars. Returns
// empty string if no tldr page exists.
func ExtractTldr(page string) string {
	name, _ := ParsePageKey(page)

	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	cacheBase := filepath.Join(home, ".cache", "tldr", "pages")
	var content []byte
	for _, platform := range []string{"osx", "common"} {
		path := filepath.Join(cacheBase, platform, name+".md")
		data, err := os.ReadFile(path)
		if err == nil {
			content = data
			break
		}
	}
	if content == nil {
		return ""
	}

	var b strings.Builder
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			b.WriteString(line[2:])
			b.WriteString(" - ")
		} else if strings.HasPrefix(line, "> ") {
			text := line[2:]
			if strings.HasPrefix(text, "More information:") {
				continue
			}
			b.WriteString(text)
			b.WriteString(" ")
		} else if strings.HasPrefix(line, "- ") {
			b.WriteString(line[2:])
			b.WriteString(" ")
		}
	}

	text := strings.TrimSpace(b.String())
	if len(text) > 500 {
		text = text[:500]
	}
	return text
}

// EnsureTldrCache runs `tldr -u` if the cache directory doesn't exist.
func EnsureTldrCache() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	cacheDir := filepath.Join(home, ".cache", "tldr", "pages")
	if _, err := os.Stat(cacheDir); err == nil {
		return
	}
	fmt.Fprintf(os.Stderr, "maneater: updating tldr cache...\n")
	cmd := exec.Command("tldr", "-u")
	cmd.Stderr = os.Stderr
	cmd.Run()
}

// LocateSource finds the roff source file by scanning manpath directories.
// This avoids a dependency on man-db's "man -w" — only mandoc is needed.
func LocateSource(manpath []string, section, page string) (string, error) {
	sections := []string{"1", "8", "5", "7", "6", "2", "3", "4"}
	if section != "" {
		sections = []string{section}
	}

	for _, dir := range manpath {
		for _, sec := range sections {
			manDir := filepath.Join(dir, "man"+sec)
			for _, ext := range []string{".gz", ""} {
				candidate := filepath.Join(manDir, page+"."+sec+ext)
				if _, err := os.Stat(candidate); err == nil {
					return candidate, nil
				}
			}
		}
	}

	return "", fmt.Errorf("no manual entry for %s", page)
}

// HashFile returns the hex SHA256 of a file's contents, or empty string on error.
func HashFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// renderMarkdown converts a roff source file to markdown via mandoc and pandoc.
// Pipeline: mandoc -T man <path> | pandoc -f man -t markdown
//
// If the mandoc pipeline fails (e.g. asciidoctor-generated roff that mandoc
// transforms into something pandoc can't parse), falls back to feeding the raw
// roff directly to pandoc.
func renderMarkdown(sourcePath string) (string, error) {
	result, err := renderMarkdownViaMandoc(sourcePath)
	if err == nil {
		return result, nil
	}

	return renderMarkdownDirect(sourcePath)
}

func renderMarkdownViaMandoc(sourcePath string) (string, error) {
	mandoc := exec.Command("mandoc", "-T", "man", sourcePath)
	pandoc := exec.Command("pandoc", "-f", "man", "-t", "markdown")

	var mandocErr bytes.Buffer
	mandoc.Stderr = &mandocErr

	pipe, err := mandoc.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("creating pipe: %w", err)
	}
	pandoc.Stdin = pipe

	var pandocOut, pandocErr bytes.Buffer
	pandoc.Stdout = &pandocOut
	pandoc.Stderr = &pandocErr

	if err := mandoc.Start(); err != nil {
		return "", fmt.Errorf("starting mandoc: %w", err)
	}
	if err := pandoc.Start(); err != nil {
		mandoc.Process.Kill()
		return "", fmt.Errorf("starting pandoc: %w", err)
	}

	mandoc.Wait()

	if err := pandoc.Wait(); err != nil {
		return "", fmt.Errorf("pandoc: %w: %s", err, pandocErr.String())
	}

	return pandocOut.String(), nil
}

func renderMarkdownDirect(sourcePath string) (string, error) {
	var reader io.Reader

	f, err := os.Open(sourcePath)
	if err != nil {
		return "", fmt.Errorf("opening source: %w", err)
	}
	defer f.Close()

	if strings.HasSuffix(sourcePath, ".gz") {
		gz, err := gzip.NewReader(f)
		if err != nil {
			return "", fmt.Errorf("decompressing source: %w", err)
		}
		defer gz.Close()
		reader = gz
	} else {
		reader = f
	}

	pandoc := exec.Command("pandoc", "-f", "man", "-t", "markdown")
	pandoc.Stdin = reader

	var pandocOut, pandocErr bytes.Buffer
	pandoc.Stdout = &pandocOut
	pandoc.Stderr = &pandocErr

	if err := pandoc.Run(); err != nil {
		return "", fmt.Errorf("pandoc: %w: %s", err, pandocErr.String())
	}

	return pandocOut.String(), nil
}

type section struct {
	Name      string
	Level     int // 1 = top-level (#), 2 = subsection (##)
	Content   string
	LineCount int
}

// splitSections splits markdown content by # and ## headers into sections.
func splitSections(markdown string) []section {
	lines := strings.Split(markdown, "\n")
	var sections []section

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		var name string
		var level int

		if strings.HasPrefix(line, "## ") {
			name = strings.TrimPrefix(line, "## ")
			level = 2
		} else if strings.HasPrefix(line, "# ") {
			name = strings.TrimPrefix(line, "# ")
			level = 1
		} else {
			if len(sections) > 0 {
				sections[len(sections)-1].Content += line + "\n"
				sections[len(sections)-1].LineCount++
			}
			continue
		}

		sections = append(sections, section{
			Name:  name,
			Level: level,
		})
	}

	for i := range sections {
		sections[i].Content = strings.TrimRight(sections[i].Content, "\n ")
	}

	return sections
}
