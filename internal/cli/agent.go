package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/situker/qqmailctl/internal/account"
	"github.com/situker/qqmailctl/internal/errmap"
	"github.com/situker/qqmailctl/internal/output"
	"github.com/situker/qqmailctl/internal/policy"
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
		commands := commandCatalog()
		data := map[string]any{
			"cli_version": rt.Build.Version, "protocol_version": "1", "schema_versions": map[string]string{"output": "1", "manifest": "1", "plan": "1"},
			"commands": commands, "risk_levels": []string{"read", "mutate", "destructive"}, "readonly": policy.Readonly(),
			"env_switches":    []string{"QQMAILCTL_READONLY", "QQMAILCTL_AUTH_CODE"},
			"untrusted_paths": []string{"data.envelopes[].subject", "data.envelopes[].from", "data.messages[].subject", "data.messages[].body", "data.attachments[].filename", "data.hits[].subject", "data.hits[].from_addr", "data.hits[].snippet", "$watch_event.envelope.subject", "$watch_event.envelope.from", "plan.items[].subject", "plan.items[].from"},
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

func commandCatalog() []agentCommand {
	read := []string{"version", "agent-info", "schema", "completion", "auth.status", "account.list", "doctor", "folder.list", "envelope.list", "message.show", "attachment.list", "search", "triage.analyze", "watch", "cache.inspect", "audit.list"}
	mutate := []string{"auth.login", "auth.logout", "account.use", "attachment.download", "export", "sync", "triage.plan", "backup", "message.mark-read", "message.move"}
	destructive := []string{"clean", "cache.clear"}
	result := make([]agentCommand, 0, len(read)+len(mutate)+len(destructive))
	for _, name := range read {
		result = append(result, agentCommand{Name: name, Risk: "read"})
	}
	for _, name := range mutate {
		result = append(result, agentCommand{Name: name, Risk: "mutate"})
	}
	for _, name := range destructive {
		result = append(result, agentCommand{Name: name, Risk: "destructive"})
	}
	return result
}

func commandNames() []string {
	result := []string{}
	for _, command := range commandCatalog() {
		result = append(result, command.Name)
	}
	return result
}
