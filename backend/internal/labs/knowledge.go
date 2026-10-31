package labs

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// KnowledgeArticle is a package-owned Markdown article in the catalog snapshot.
type KnowledgeArticle struct {
	ID    string `yaml:"id" json:"id"`
	Title string `yaml:"title" json:"title"`
	File  string `yaml:"file" json:"-"`
	Body  string `yaml:"-" json:"body"`
}

// KnowledgeSection preserves the author's section and article order.
type KnowledgeSection struct {
	ID          string             `yaml:"id" json:"id"`
	Heading     string             `yaml:"heading" json:"heading"`
	Description string             `yaml:"description,omitempty" json:"description,omitempty"`
	Icon        string             `yaml:"icon,omitempty" json:"icon,omitempty"`
	Accent      string             `yaml:"accent,omitempty" json:"accent,omitempty"`
	Articles    []KnowledgeArticle `yaml:"articles" json:"articles"`
}

// knowledgeFile confines both lexical paths and symlink targets to root.
func knowledgeFile(root, name string) ([]byte, error) {
	if name == "" || filepath.IsAbs(name) || strings.Contains(name, `\`) {
		return nil, fmt.Errorf("invalid relative knowledge path %q", name)
	}
	for _, part := range strings.Split(filepath.ToSlash(name), "/") {
		if part == ".." {
			return nil, fmt.Errorf("knowledge traversal %q", name)
		}
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	realPath, err := filepath.EvalSymlinks(filepath.Join(root, name))
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(realRoot, realPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("knowledge path %q escapes its directory", name)
	}
	info, err := os.Stat(realPath)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("knowledge path %q is not a regular file", name)
	}
	data, err := os.ReadFile(realPath)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(string(data)) == "" {
		return nil, fmt.Errorf("knowledge file %q is empty", name)
	}
	return data, nil
}

var knowledgeLinks = []*regexp.Regexp{
	regexp.MustCompile(`\]\(\s*<?(?:/knowledge)?#([^\s)>]+)>?(?:\s+(?:"[^"]*"|'[^']*'|\([^)]*\)))?\s*\)`),
	regexp.MustCompile(`(?m)^[ \t]*\[[^]\n]+\]:[ \t]*<?(?:/knowledge)?#([^\s>]+)>?`),
}

func validateKnowledgeLinks(text string, ids map[string]bool) error {
	for _, pattern := range knowledgeLinks {
		for _, match := range pattern.FindAllStringSubmatch(text, -1) {
			if !ids[match[1]] {
				return fmt.Errorf("unknown knowledge article %q", match[1])
			}
		}
	}
	return nil
}

func loadKnowledge(packageDir, index, scenarios string) ([]KnowledgeSection, error) {
	// The index and every body belong to this package's knowledge subtree.
	if !strings.HasPrefix(index, "knowledge/") {
		return nil, fmt.Errorf("knowledge must name an index under knowledge/")
	}
	data, err := knowledgeFile(packageDir, index)
	if err != nil {
		return nil, err
	}
	// Even a symlinked knowledge directory must remain within the package.
	root := filepath.Join(packageDir, "knowledge")
	if _, err := knowledgeFile(root, strings.TrimPrefix(index, "knowledge/")); err != nil {
		return nil, err
	}
	var doc struct {
		Sections []KnowledgeSection `yaml:"sections"`
	}
	if err := decodeStrict(data, &doc); err != nil {
		return nil, err
	}
	if len(doc.Sections) == 0 {
		return nil, fmt.Errorf("knowledge sections must not be empty")
	}
	sections, articles := map[string]bool{}, map[string]bool{}
	for i := range doc.Sections {
		section := &doc.Sections[i]
		if !slugPattern.MatchString(section.ID) || sections[section.ID] || strings.TrimSpace(section.Heading) == "" || len(section.Articles) == 0 {
			return nil, fmt.Errorf("invalid or duplicate knowledge section %q", section.ID)
		}
		sections[section.ID] = true
		if !oneOf(section.Icon, "", "zap", "shield", "radio", "wrench", "layers", "target", "book") || !oneOf(section.Accent, "", "sky", "emerald", "violet", "amber", "slate", "rose") {
			return nil, fmt.Errorf("invalid icon or accent for knowledge section %q", section.ID)
		}
		for j := range section.Articles {
			article := &section.Articles[j]
			if !slugPattern.MatchString(article.ID) || articles[article.ID] || strings.TrimSpace(article.Title) == "" {
				return nil, fmt.Errorf("invalid or duplicate knowledge article %q", article.ID)
			}
			articles[article.ID] = true
			// Check from the package as well as the knowledge root, preventing a
			// symlinked subtree from redefining the confinement boundary.
			if _, err := knowledgeFile(packageDir, "knowledge/"+article.File); err != nil {
				return nil, err
			}
			body, err := knowledgeFile(root, article.File)
			if err != nil {
				return nil, err
			}
			article.Body = string(body)
		}
	}
	for _, section := range doc.Sections {
		for _, article := range section.Articles {
			if err := validateKnowledgeLinks(article.Body, articles); err != nil {
				return nil, fmt.Errorf("article %s: %w", article.ID, err)
			}
		}
	}
	scenarioFiles, err := loadScenarioFiles(filepath.Join(packageDir, scenarios))
	if err != nil {
		return nil, err
	}
	for _, scenario := range scenarioFiles {
		text, err := yaml.Marshal(scenario)
		if err != nil {
			return nil, err
		}
		if err := validateKnowledgeLinks(string(text), articles); err != nil {
			return nil, fmt.Errorf("scenario %s: %w", scenario.ID, err)
		}
	}
	return doc.Sections, nil
}

func oneOf(value string, options ...string) bool {
	for _, option := range options {
		if option == value {
			return true
		}
	}
	return false
}
