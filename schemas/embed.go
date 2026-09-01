package schemas

import (
	"embed"
	"fmt"
	"sort"
)

//go:embed *.json
var files embed.FS

func Get(name string) ([]byte, error) {
	if name == "" {
		return nil, fmt.Errorf("schema name is empty")
	}
	return files.ReadFile(name)
}

func Names() []string {
	entries, _ := files.ReadDir(".")
	result := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			result = append(result, entry.Name())
		}
	}
	sort.Strings(result)
	return result
}
