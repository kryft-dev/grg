package filter

import (
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// FileTypeDef defines file extensions and exact filenames associated with a file type identifier.
type FileTypeDef struct {
	Extensions []string
	Filenames  []string
}

// builtinTypes provides standard file type mappings matching ripgrep conventions.
// Treat it as immutable: read it only through LookupType and ListTypes, which
// hand out defensive copies so callers cannot mutate the table.
var builtinTypes = map[string]FileTypeDef{
	"c":          {Extensions: []string{".c", ".h"}},
	"cpp":        {Extensions: []string{".cpp", ".cc", ".cxx", ".c++", ".hpp", ".hh", ".hxx", ".h++"}},
	"css":        {Extensions: []string{".css", ".scss", ".sass", ".less"}},
	"docker":     {Extensions: []string{".dockerfile"}, Filenames: []string{"Dockerfile", "Containerfile"}},
	"dockerfile": {Extensions: []string{".dockerfile"}, Filenames: []string{"Dockerfile", "Containerfile"}},
	"go":         {Extensions: []string{".go"}},
	"html":       {Extensions: []string{".html", ".htm"}},
	"java":       {Extensions: []string{".java"}},
	"js":         {Extensions: []string{".js", ".mjs", ".cjs", ".jsx"}},
	"javascript": {Extensions: []string{".js", ".mjs", ".cjs", ".jsx"}},
	"json":       {Extensions: []string{".json"}},
	"kotlin":     {Extensions: []string{".kt", ".kts"}},
	"lua":        {Extensions: []string{".lua"}},
	"make":       {Extensions: []string{".mk"}, Filenames: []string{"Makefile", "makefile", "GNUmakefile"}},
	"makefile":   {Extensions: []string{".mk"}, Filenames: []string{"Makefile", "makefile", "GNUmakefile"}},
	"md":         {Extensions: []string{".md", ".markdown"}},
	"markdown":   {Extensions: []string{".md", ".markdown"}},
	"perl":       {Extensions: []string{".pl", ".pm"}},
	"php":        {Extensions: []string{".php", ".phtml"}},
	"py":         {Extensions: []string{".py", ".pyw", ".pyi"}},
	"python":     {Extensions: []string{".py", ".pyw", ".pyi"}},
	"ruby":       {Extensions: []string{".rb", ".rake"}},
	"rust":       {Extensions: []string{".rs"}},
	"scala":      {Extensions: []string{".scala"}},
	"sh":         {Extensions: []string{".sh", ".bash", ".zsh"}},
	"bash":       {Extensions: []string{".sh", ".bash", ".zsh"}},
	"sql":        {Extensions: []string{".sql"}},
	"swift":      {Extensions: []string{".swift"}},
	"toml":       {Extensions: []string{".toml"}},
	"ts":         {Extensions: []string{".ts", ".mts", ".cts", ".tsx"}},
	"typescript": {Extensions: []string{".ts", ".mts", ".cts", ".tsx"}},
	"xml":        {Extensions: []string{".xml", ".xsd", ".xsl"}},
	"yaml":       {Extensions: []string{".yaml", ".yml"}},
	"yml":        {Extensions: []string{".yaml", ".yml"}},
	"zig":        {Extensions: []string{".zig"}},
}

// TypeMatcher filters files based on one or more file types.
type TypeMatcher struct {
	selectedExts      map[string]bool
	selectedFilenames map[string]bool
}

// NewTypeMatcher creates a TypeMatcher for the given type names.
func NewTypeMatcher(types []string) (*TypeMatcher, error) {
	if len(types) == 0 {
		return nil, nil
	}

	tm := &TypeMatcher{
		selectedExts:      make(map[string]bool),
		selectedFilenames: make(map[string]bool),
	}

	for _, t := range types {
		typeName := strings.ToLower(strings.TrimSpace(t))
		if typeName == "" {
			continue
		}

		def, ok := LookupType(typeName)
		if !ok {
			return nil, fmt.Errorf("unknown file type %q (use --type-list to view supported types)", t)
		}

		for _, ext := range def.Extensions {
			tm.selectedExts[strings.ToLower(ext)] = true
		}
		for _, fn := range def.Filenames {
			tm.selectedFilenames[fn] = true
		}
	}

	return tm, nil
}

// Match checks if path matches any of the configured file types.
func (tm *TypeMatcher) Match(path string) bool {
	if tm == nil || (len(tm.selectedExts) == 0 && len(tm.selectedFilenames) == 0) {
		return true
	}

	base := filepath.Base(path)

	// 1. Exact filename check (e.g. Dockerfile, Makefile)
	if tm.selectedFilenames[base] {
		return true
	}

	// 2. Extension check
	ext := strings.ToLower(filepath.Ext(base))
	if ext != "" && tm.selectedExts[ext] {
		return true
	}

	return false
}

// LookupType returns the definition for the named file type. The returned
// FileTypeDef owns copies of its slices, so callers may mutate them without
// corrupting the builtin table.
func LookupType(name string) (FileTypeDef, bool) {
	def, ok := builtinTypes[name]
	if !ok {
		return FileTypeDef{}, false
	}
	return FileTypeDef{
		Extensions: slices.Clone(def.Extensions),
		Filenames:  slices.Clone(def.Filenames),
	}, true
}

// ListTypes returns a sorted list of all supported file type names.
func ListTypes() []string {
	names := make([]string, 0, len(builtinTypes))
	for name := range builtinTypes {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
