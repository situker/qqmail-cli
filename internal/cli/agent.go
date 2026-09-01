package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/situker/qqmailctl/internal/account"
	"github.com/situker/qqmailctl/internal/errmap"
	"github.com/situker/qqmailctl/internal/output"
	"github.com/situker/qqmailctl/schemas"
	"github.com/spf13/cobra"
)

type agentCommand struct {
	Name string `json:"name"`
	Risk string `json:"risk"`
}

func newAgentInfoCommand(rt *Runtime) *cobra.Command {
	cmd := &cobra.Command{Use: "agent-info", Short: "Emit the machine-readable capability contract"}
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		configured := false
		accountInfo := map[string]any{"name": "", "configured": false}
		if cfg, _, err := account.Load(rt.ConfigPath); err == nil {
			if named, err := cfg.Resolve(rt.Account); err == nil {
				configured = true
				accountInfo = map[string]any{"name": named.Name, "configured": true}
			}
		}
		commands := []agentCommand{}
		for _, name := range readonlyCommandNames() {
			commands = append(commands, agentCommand{Name: name, Risk: "read"})
		}
		data := map[string]any{
			"cli_version": rt.Build.Version, "protocol_version": "1", "schema_versions": map[string]string{"output": "1", "manifest": "1", "plan": "1"},
			"commands": commands, "risk_levels": []string{"read"}, "readonly": true,
			"env_switches":    []string{"QQMAILCTL_READONLY", "QQMAILCTL_AUTH_CODE"},
			"untrusted_paths": []string{"data.envelopes[].subject", "data.envelopes[].from", "data.messages[].subject", "data.messages[].body", "data.attachments[].filename"},
			"account":         accountInfo, "configured": configured,
		}
		// agent-info is JSON-only by contract, regardless of the global flag.
		return writeDetailed(rt, cmd, data, nil, output.Meta{})
	}
	return cmd
}

func newSchemaCommand(rt *Runtime) *cobra.Command {
	aliases := map[string]string{
		"version": "version.schema.json", "manifest": "manifest.schema.json", "plan": "plan.schema.json", "common": "envelope-common.schema.json",
		"agent-info": "agent-info.schema.json",
	}
	cmd := &cobra.Command{Use: "schema [command]", Args: cobra.MaximumNArgs(1), Short: "Emit an embedded JSON Schema"}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return json.NewEncoder(rt.Out).Encode(map[string]any{"schema_version": "1", "schemas": schemas.Names()})
		}
		name := strings.ReplaceAll(args[0], " ", ".")
		filename, ok := aliases[name]
		if !ok {
			filename = name + ".schema.json"
		}
		raw, err := schemas.Get(filename)
		if err != nil {
			return &errmap.Error{Kind: errmap.NotFound, Message: fmt.Sprintf("没有命令 %q 的 schema", args[0])}
		}
		var value any
		if err := json.Unmarshal(raw, &value); err != nil {
			return err
		}
		return json.NewEncoder(rt.Out).Encode(value)
	}
	return cmd
}

func readonlyCommandNames() []string {
	return []string{"version", "agent-info", "schema", "completion", "auth.login", "auth.status", "auth.logout", "account.list", "account.use", "doctor", "folder.list", "envelope.list", "message.show", "attachment.list", "attachment.download", "export"}
}
