package cleanupplan

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/situker/qqmail-cli/internal/mailmodel"
	projectschemas "github.com/situker/qqmail-cli/schemas"
)

const Schema = 1

type Item struct {
	ID         string              `json:"id"`
	Category   string              `json:"category"`
	Confidence float64             `json:"confidence,omitempty"`
	Reason     string              `json:"reason"`
	Evidence   []string            `json:"evidence"`
	From       []mailmodel.Address `json:"from"`
	Subject    string              `json:"subject"`
	Date       time.Time           `json:"date"`
	SizeBytes  int64               `json:"size_bytes"`
}

type Statistics struct {
	TotalCount     int            `json:"total_count"`
	TotalSizeBytes int64          `json:"total_size_bytes"`
	ByCategory     map[string]int `json:"by_category"`
	ByFromDomain   map[string]int `json:"by_from_domain"`
}

type Plan struct {
	Schema     int        `json:"schema"`
	CreatedAt  time.Time  `json:"created_at"`
	Items      []Item     `json:"items"`
	Statistics Statistics `json:"statistics"`
	BackupRoot string     `json:"backup_root,omitempty"`
}

func Validate(raw []byte) error {
	document, err := projectschemas.Get("plan.schema.json")
	if err != nil {
		return err
	}
	var schemaValue, value any
	if err := json.Unmarshal(document, &schemaValue); err != nil {
		return err
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	compiler := jsonschema.NewCompiler()
	const resource = "https://github.com/situker/qqmail-cli/schemas/plan.schema.json"
	if err := compiler.AddResource(resource, schemaValue); err != nil {
		return err
	}
	compiled, err := compiler.Compile(resource)
	if err != nil {
		return err
	}
	return compiled.Validate(value)
}

func Load(path string) (Plan, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Plan{}, err
	}
	if err := Validate(raw); err != nil {
		return Plan{}, fmt.Errorf("plan schema validation failed: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var plan Plan
	if err := decoder.Decode(&plan); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

func Save(path string, plan Plan) error {
	if plan.Schema == 0 {
		plan.Schema = Schema
	}
	if plan.CreatedAt.IsZero() {
		plan.CreatedAt = time.Now().UTC()
	}
	raw, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return err
	}
	if err := Validate(raw); err != nil {
		return fmt.Errorf("plan schema validation failed: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".plan-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(raw, '\n')); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err == nil {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Rename(tmpPath, path)
}
