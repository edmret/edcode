package configure

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/edmundo/edcode/internal/banner"
	"golang.org/x/term"
	"gopkg.in/yaml.v3"
)

type ProviderDef struct {
	Label        string
	Name         string
	BaseURL      string
	DefaultModel string
	NeedsKey     bool
	IsCustom     bool
	CustomName   string
}

var knownProviders = []ProviderDef{
	{Label: "OpenAI", Name: "openai", BaseURL: "https://api.openai.com/v1", DefaultModel: "gpt-4o", NeedsKey: true},
	{Label: "Anthropic", Name: "anthropic", BaseURL: "https://api.anthropic.com/v1", DefaultModel: "claude-sonnet-4-20250514", NeedsKey: true},
	{Label: "OpenRouter", Name: "openrouter", BaseURL: "https://openrouter.ai/api/v1", DefaultModel: "anthropic/claude-sonnet-4-20250514", NeedsKey: true},
	{Label: "Ollama (local)", Name: "ollama", BaseURL: "http://localhost:11434/v1", DefaultModel: "llama3", NeedsKey: false},
	{Label: "Custom OpenAI-compatible", Name: "", BaseURL: "", DefaultModel: "gpt-4o", NeedsKey: false, IsCustom: true},
}

func Run() error {
	reader := bufio.NewReader(os.Stdin)

	existing, _ := loadExistingConfig()

	fmt.Println()
	banner.Print()
	fmt.Println()
	fmt.Println("  Provider Setup")
	fmt.Println("  ===============")
	fmt.Println()

	selected := multiSelect("Select providers to configure:", knownProviders)
	if len(selected) == 0 {
		fmt.Println("  No providers selected.")
		agentConfig(reader, existing, nil)
		if err := writeConfig(existing); err != nil {
			return fmt.Errorf("write config: %w", err)
		}
		return nil
	}

	for _, p := range selected {
		configureProvider(reader, p, existing)
	}

	allModels := collectModels(existing)
	agentConfig(reader, existing, allModels)

	if err := writeConfig(existing); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	fmt.Println()
	fmt.Println("  Configuration saved to edcode.yaml")
	fmt.Println()
	return nil
}

func loadExistingConfig() (map[string]any, error) {
	data, err := os.ReadFile("edcode.yaml")
	if err != nil {
		return map[string]any{}, nil
	}
	var cfg map[string]any
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return map[string]any{}, nil
	}
	return cfg, nil
}

func configureProvider(reader *bufio.Reader, def ProviderDef, cfg map[string]any) {
	fmt.Println()
	fmt.Println("  Provider: " + def.Label)
	fmt.Println()

	providers := ensureMap(cfg, "providers")

	if def.IsCustom {
		configureCustom(reader, providers)
		return
	}

	entry := ensureMap(providers, def.Name)

	if def.NeedsKey {
		existingKey, _ := entry["api_key"].(string)
		prompt := "  API key"
		if existingKey != "" {
			prompt += " (leave empty to keep existing)"
		}
		prompt += ": "
		key := promptInput(reader, prompt)
		if key != "" {
			entry["api_key"] = key
		} else if existingKey != "" {
			entry["api_key"] = existingKey
		} else {
			fmt.Println("  No API key provided. Set " + strings.ToUpper(def.Name) + "_API_KEY environment variable later.")
		}
	}

	entry["base_url"] = def.BaseURL
	entry["default_model"] = def.DefaultModel

	discovered := discoverModels(def.BaseURL, getAPIKey(entry))
	if len(discovered) > 0 {
		fmt.Printf("  Discovered %d models from %s\n", len(discovered), def.Label)
		useDiscovered := promptBool(reader, "  Use discovered models?", true)
		if useDiscovered {
			entry["models"] = discovered
		}
	}

	providers[def.Name] = entry
}

func configureCustom(reader *bufio.Reader, providers map[string]any) {
	name := promptInput(reader, "  Custom provider name (e.g. litellm, vllm): ")
	if name == "" {
		fmt.Println("  Skipping custom provider.")
		return
	}

	entry := ensureMap(providers, name)

	baseURL := promptInput(reader, "  Base URL (e.g. http://localhost:4000/v1): ")
	if baseURL == "" {
		fmt.Println("  Skipping custom provider.")
		delete(providers, name)
		return
	}
	entry["base_url"] = baseURL

	hasKey := promptBool(reader, "  Requires API key?", false)
	if hasKey {
		key := promptInput(reader, "  API key: ")
		if key != "" {
			entry["api_key"] = key
		}
	}

	defaultModel := promptInput(reader, "  Default model (e.g. gpt-4o-mini): ")
	if defaultModel != "" {
		entry["default_model"] = defaultModel
	}

	discovered := discoverModels(baseURL, getAPIKey(entry))
	if len(discovered) > 0 {
		fmt.Printf("  Discovered %d models from %s\n", len(discovered), name)
		useDiscovered := promptBool(reader, "  Use discovered models?", true)
		if useDiscovered {
			entry["models"] = discovered
		}
	} else {
		fmt.Println("  Could not auto-discover models. Add them manually to edcode.yaml.")
	}
}

func discoverModels(baseURL, apiKey string) []map[string]any {
	url := strings.TrimRight(baseURL, "/") + "/models"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil
	}
	if len(result.Data) == 0 {
		return nil
	}

	var models []map[string]any
	for _, m := range result.Data {
		models = append(models, map[string]any{
			"id":   m.ID,
			"name": m.ID,
		})
	}
	if len(models) > 50 {
		models = models[:50]
	}
	return models
}

func writeConfig(cfg map[string]any) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	header := "# Edcode Configuration\n"
	return os.WriteFile("edcode.yaml", []byte(header+string(data)), 0644)
}

func ensureMap(m map[string]any, key string) map[string]any {
	if v, ok := m[key]; ok {
		if vm, ok := v.(map[string]any); ok {
			return vm
		}
	}
	entry := make(map[string]any)
	m[key] = entry
	return entry
}

func getAPIKey(entry map[string]any) string {
	if key, ok := entry["api_key"].(string); ok {
		return key
	}
	return ""
}

type AgentAssignment struct {
	Name        string
	Description string
	DefaultRole string
}

var mandatoryAgents = []AgentAssignment{
	{Name: "default", Description: "General-purpose assistant", DefaultRole: "default"},
	{Name: "planner", Description: "Analyzes tasks & creates plans", DefaultRole: "planner"},
	{Name: "coder", Description: "Writes and edits code", DefaultRole: "coder"},
	{Name: "reviewer", Description: "Reviews changes for quality", DefaultRole: "reviewer"},
}

func collectModels(cfg map[string]any) []string {
	providers, _ := cfg["providers"].(map[string]any)
	if providers == nil {
		return nil
	}
	seen := map[string]bool{}
	var models []string
	for name, p := range providers {
		pm, ok := p.(map[string]any)
		if !ok {
			continue
		}
		if defaultModel, ok := pm["default_model"].(string); ok && defaultModel != "" {
			ref := name + "/" + defaultModel
			if !seen[ref] {
				models = append(models, ref)
				seen[ref] = true
			}
		}
		if ms, ok := pm["models"]; ok {
			collectModelIDs(ms, name, &models, &seen)
		}
	}
	return models
}

func collectModelIDs(raw any, providerName string, models *[]string, seen *map[string]bool) {
	switch list := raw.(type) {
	case []any:
		for _, m := range list {
			addModelID(m, providerName, models, seen)
		}
	case []map[string]any:
		for _, m := range list {
			addModelID(m, providerName, models, seen)
		}
	}
}

func addModelID(m any, providerName string, models *[]string, seen *map[string]bool) {
	if mm, ok := m.(map[string]any); ok {
		if id, ok := mm["id"].(string); ok && id != "" {
			ref := providerName + "/" + id
			if !(*seen)[ref] {
				*models = append(*models, ref)
				(*seen)[ref] = true
			}
		}
	}
}

func agentConfig(reader *bufio.Reader, cfg map[string]any, availableModels []string) {
	fmt.Println()
	banner.Print()
	fmt.Println()
	fmt.Println("  Agent Configuration")
	fmt.Println("  ====================")
	fmt.Println()

	agents := ensureMap(cfg, "agents")

	if len(availableModels) == 0 {
		fmt.Println("  No models available. Skipping agent configuration.")
		fmt.Println("  Run `edcode --configure` later to set up agents.")
		return
	}

	fmt.Println("  Available models:")
	for i, m := range availableModels {
		fmt.Printf("    %d. %s\n", i+1, m)
	}
	fmt.Println()

	for _, a := range mandatoryAgents {
		existing := ensureMap(agents, a.Name)
		existingDesc, _ := existing["description"].(string)
		if existingDesc == "" {
			existing["description"] = a.Description
		}
		if _, ok := existing["model"]; ok && promptBool(reader, fmt.Sprintf("  Agent %q already configured (model: %v). Change?", a.Name, existing["model"]), false) {
			pickModel(reader, existing, a.Name, availableModels)
		} else if _, ok := existing["model"]; !ok {
			pickModel(reader, existing, a.Name, availableModels)
		}
		agents[a.Name] = existing
	}

	fmt.Println("  Agent configuration complete.")
}

func pickModel(reader *bufio.Reader, entry map[string]any, agentName string, models []string) {
	if len(models) == 1 {
		entry["model"] = models[0]
		fmt.Printf("  Using only available model %q for %q\n", models[0], agentName)
		return
	}
	for i, m := range models {
		fmt.Printf("    %d. %s\n", i+1, m)
	}
	fmt.Print("  Choice [1]: ")
	text, _ := reader.ReadString('\n')
	text = strings.TrimSpace(text)
	var idx int
	if _, err := fmt.Sscanf(text, "%d", &idx); err == nil && idx >= 1 && idx <= len(models) {
		entry["model"] = models[idx-1]
		fmt.Printf("  Assigned %q to %q\n", models[idx-1], agentName)
	} else {
		entry["model"] = models[0]
		fmt.Printf("  Using default: %q\n", models[0])
	}
}

func promptInput(reader *bufio.Reader, prompt string) string {
	fmt.Print(prompt)
	text, _ := reader.ReadString('\n')
	return strings.TrimSpace(text)
}

func promptBool(reader *bufio.Reader, prompt string, def bool) bool {
	suffix := " [Y/n]"
	if !def {
		suffix = " [y/N]"
	}
	fmt.Print(prompt + suffix + " ")
	text, _ := reader.ReadString('\n')
	text = strings.TrimSpace(strings.ToLower(text))
	if text == "" {
		return def
	}
	return text == "y" || text == "yes"
}

func multiSelect(prompt string, items []ProviderDef) []ProviderDef {
	// Calculate max label length for alignment
	maxLabelLen := 0
	for _, p := range items {
		label := p.Label
		if p.IsCustom {
			label += " (custom)"
		}
		if len(label) > maxLabelLen {
			maxLabelLen = len(label)
		}
	}

	fullRender := func(sel []bool, cur int) string {
		var out string
		out += "\033[H\033[J"
		// Logo starts with \n in the raw string literal. Split on \n and
		// rejoin with \r\n so each line starts at column 0 in raw mode.
		logoLines := strings.Split(banner.Logo, "\n")
		for _, line := range logoLines {
			out += line + "\r\n"
		}
		out += prompt + "\r\n"
		out += "\r\n"
		out += "  up/down: navigate  |  space: toggle  |  enter: confirm  |  q: quit\r\n"
		out += "\r\n"
		for i, p := range items {
			marker := " "
			if i == cur {
				marker = ">"
			}
			checkbox := "[ ]"
			if sel[i] {
				checkbox = "[x]"
			}
			label := p.Label
			if p.IsCustom {
				label += " (custom)"
			}
			padded := label + strings.Repeat(" ", maxLabelLen-len(label))
			out += fmt.Sprintf("  %s %s  %s\r\n", marker, checkbox, padded)
		}
		out += fmt.Sprintf("  %d selected\r\n", countSelected(sel))
		return out
	}

	// Print initial state before raw mode using \r\n (works in both cooked and raw)
	fmt.Print(fullRender(make([]bool, len(items)), 0))

	// Enter raw mode for keystroke reading
	fd := int(os.Stdin.Fd())
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		fmt.Println()
		fallbackMultiSelect(prompt, items)
		os.Exit(0)
		return nil
	}
	defer term.Restore(fd, oldState)

	// Hide cursor
	fmt.Print("\033[?25l")

	selected := make([]bool, len(items))
	cursor := 0

	buf := make([]byte, 3)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil || n == 0 {
			break
		}

		changed := false

		// Ctrl+C or Ctrl+D
		if buf[0] == 3 || buf[0] == 4 {
			fmt.Print("\033[?25h")
			term.Restore(fd, oldState)
			fmt.Println()
			os.Exit(1)
		}

		// Enter
		if buf[0] == 13 || buf[0] == 10 {
			fmt.Print("\033[?25h")
			var result []ProviderDef
			for i, sel := range selected {
				if sel {
					result = append(result, items[i])
				}
			}
			return result
		}

		// Escape
		if buf[0] == 27 && n == 1 {
			fmt.Print("\033[?25h")
			return nil
		}

		// q
		if buf[0] == 113 || buf[0] == 81 {
			fmt.Print("\033[?25h")
			return nil
		}

		// Space - toggle
		if buf[0] == 32 {
			selected[cursor] = !selected[cursor]
			changed = true
		}

		// Arrow keys
		if n >= 3 && buf[0] == 27 && buf[1] == 91 {
			switch buf[2] {
			case 65: // up
				if cursor > 0 {
					cursor--
					changed = true
				}
			case 66: // down
				if cursor < len(items)-1 {
					cursor++
					changed = true
				}
			}
		}

		if changed {
			out := fullRender(selected, cursor)
			fmt.Print(out)
		}
	}

	fmt.Print("\033[?25h")
	return nil
}

func countSelected(sel []bool) int {
	count := 0
	for _, s := range sel {
		if s {
			count++
		}
	}
	return count
}

func fallbackMultiSelect(prompt string, items []ProviderDef) []ProviderDef {
	reader := bufio.NewReader(os.Stdin)
	fmt.Println()
	fmt.Println(prompt)
	fmt.Println()

	maxLabelLen := 0
	for _, p := range items {
		label := p.Label
		if p.IsCustom {
			label += " (custom)"
		}
		if len(label) > maxLabelLen {
			maxLabelLen = len(label)
		}
	}

	for i, p := range items {
		label := p.Label
		if p.IsCustom {
			label += " (custom)"
		}
		padded := label + strings.Repeat(" ", maxLabelLen-len(label))
		fmt.Printf("  [%d] %s\n", i+1, padded)
	}
	fmt.Println()
	fmt.Print("  Enter numbers to select (comma-separated, e.g. 1,3): ")
	text, _ := reader.ReadString('\n')
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	parts := strings.Split(text, ",")
	var selected []ProviderDef
	for _, part := range parts {
		part = strings.TrimSpace(part)
		idx, err := fmt.Sscanf(part, "%d", new(int))
		if err == nil && idx == 1 {
			var n int
			fmt.Sscanf(part, "%d", &n)
			if n >= 1 && n <= len(items) {
				selected = append(selected, items[n-1])
			}
		}
	}
	return selected
}
