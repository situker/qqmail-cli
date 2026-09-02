package schemas

import (
	"encoding/json"
	"testing"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

func TestEveryEmbeddedSchemaCompiles(t *testing.T) {
	compiler := jsonschema.NewCompiler()
	const base = "https://github.com/situker/qqmail-cli/schemas/"
	for _, name := range Names() {
		raw, err := Get(name)
		if err != nil {
			t.Fatal(err)
		}
		var document any
		if err := json.Unmarshal(raw, &document); err != nil {
			t.Fatalf("%s is not JSON: %v", name, err)
		}
		if err := compiler.AddResource(base+name, document); err != nil {
			t.Fatalf("add %s: %v", name, err)
		}
	}
	for _, name := range Names() {
		if _, err := compiler.Compile(base + name); err != nil {
			t.Errorf("compile %s: %v", name, err)
		}
	}
}
