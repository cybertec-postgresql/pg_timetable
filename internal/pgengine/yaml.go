package pgengine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// YamlChain represents a chain with tasks for YAML processing
type YamlChain struct {
	Chain      `yaml:",inline"`
	ClientName string     `db:"client_name" yaml:"client_name,omitempty"`
	Schedule   string     `db:"run_at" yaml:"schedule,omitempty"`
	Live       bool       `db:"live" yaml:"live,omitempty"`
	Tasks      []YamlTask `yaml:"tasks"`
}

// YamlTask extends the basic task structure with Parameters field
type YamlTask struct {
	ChainTask  `yaml:",inline"`
	TaskName   string `db:"task_name" yaml:"name,omitempty"`
	Parameters []any  `yaml:"parameters,omitempty"`
}

// YamlConfig represents the root YAML configuration
type YamlConfig struct {
	Chains []YamlChain `yaml:"chains"`
}

// LoadYamlChains loads chains from a YAML file and imports them
func (pge *PgEngine) LoadYamlChains(ctx context.Context, filePath string, replace bool) error {
	// Parse YAML file
	yamlConfig, err := ParseYamlFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to parse YAML file: %w", err)
	}

	// Import chains
	for _, yamlChain := range yamlConfig.Chains {
		// Delete existing chain if replace mode
		if replace {
			_, _ = pge.ConfigDb.Exec(ctx, "SELECT timetable.delete_job($1)", yamlChain.ChainName)
		}

		// Check if chain exists
		var exists bool
		err := pge.ConfigDb.QueryRow(ctx,
			"SELECT EXISTS(SELECT 1 FROM timetable.chain WHERE chain_name = $1)",
			yamlChain.ChainName).Scan(&exists)
		if err != nil {
			return fmt.Errorf("failed to check if chain exists: %w", err)
		}
		if exists && !replace {
			return fmt.Errorf("chain '%s' already exists (use --replace flag to overwrite)", yamlChain.ChainName)
		}

		// Multi-task chain - use direct SQL
		chainID, err := pge.createChainFromYaml(ctx, &yamlChain)
		if err != nil {
			return fmt.Errorf("failed to create multi-task chain %s: %w", yamlChain.ChainName, err)
		}
		pge.l.WithField("chain", yamlChain.ChainName).WithField("chain_id", chainID).Info("Created multi-task chain")
	}

	pge.l.WithField("chains", len(yamlConfig.Chains)).WithField("file", filePath).Info("Successfully imported YAML chains")
	return nil
}

// createChainFromYaml creates a multi-task chain using direct SQL inserts
func (pge *PgEngine) createChainFromYaml(ctx context.Context, yamlChain *YamlChain) (int64, error) {
	// Insert chain
	var chainID int64
	err := pge.ConfigDb.QueryRow(ctx, `
		INSERT INTO timetable.chain (
			chain_name, run_at, max_instances, timeout, live, 
			self_destruct, exclusive_execution, client_name, on_error
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) 
		RETURNING chain_id`,
		yamlChain.ChainName,
		yamlChain.Schedule,
		yamlChain.MaxInstances,
		yamlChain.Timeout,
		yamlChain.Live,
		yamlChain.SelfDestruct,
		yamlChain.ExclusiveExecution,
		nullString(yamlChain.ClientName),
		nullString(yamlChain.OnError)).Scan(&chainID)
	if err != nil {
		return 0, fmt.Errorf("failed to insert chain: %w", err)
	}

	// Insert tasks
	for i, task := range yamlChain.Tasks {
		taskOrder := float64((i + 1) * 10)

		var taskID int64
		err := pge.ConfigDb.QueryRow(ctx, `
			INSERT INTO timetable.task (
				chain_id, task_order, task_name, kind, command, 
				run_as, database_connection, ignore_error, autonomous, timeout
			) VALUES ($1, $2, $3, $4::timetable.command_kind, $5, $6, $7, $8, $9, $10) 
			RETURNING task_id`,
			chainID,
			taskOrder,
			nullString(task.TaskName),
			task.Kind,
			task.Command,
			nullString(task.RunAs),
			nullString(task.ConnectString),
			task.IgnoreError,
			task.Autonomous,
			task.Timeout).Scan(&taskID)
		if err != nil {
			return 0, fmt.Errorf("failed to insert task %d: %w", i+1, err)
		}

		// Insert parameters if any
		if len(task.Parameters) > 0 {
			params, err := task.ToSQLParameters()
			if err != nil {
				return 0, fmt.Errorf("failed to convert parameters: %w", err)
			}
			_, err = pge.ConfigDb.Exec(ctx,
				"INSERT INTO timetable.parameter (task_id, order_id, value) VALUES ($1, 1, $2::jsonb)",
				taskID, params)
			if err != nil {
				return 0, fmt.Errorf("failed to insert parameters: %w", err)
			}
		}
	}

	return chainID, nil
}

// nullString returns nil for empty strings, otherwise returns the string
func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// ValidateChain validates a YAML chain configuration
func (c *YamlChain) ValidateChain() error {
	if c.ChainName == "" {
		return fmt.Errorf("chain name is required")
	}

	if c.Schedule == "" {
		return fmt.Errorf("chain schedule is required")
	}

	// Validate cron format
	specialSchedules := []string{"@reboot", "@after", "@every"}
	isSpecial := false
	for _, s := range specialSchedules {
		if strings.HasPrefix(c.Schedule, s) {
			isSpecial = true
			break
		}
	}

	if !isSpecial {
		fields := strings.Fields(c.Schedule)
		if len(fields) != 5 {
			return fmt.Errorf("invalid cron format: %s (expected 5 fields)", c.Schedule)
		}
	}

	if len(c.Tasks) == 0 {
		return fmt.Errorf("chain must have at least one task")
	}

	// Validate each task
	for i, task := range c.Tasks {
		if err := task.ValidateTask(); err != nil {
			return fmt.Errorf("task %d: %w", i+1, err)
		}
	}

	return nil
}

// ValidateTask validates a YAML task configuration
func (t *YamlTask) ValidateTask() error {
	if t.Command == "" {
		return fmt.Errorf("task command is required")
	}

	// Validate kind
	switch strings.ToUpper(t.Kind) {
	case "", "SQL", "PROGRAM", "BUILTIN":
		// Valid kinds
	default:
		return fmt.Errorf("invalid task kind: %s (must be SQL, PROGRAM, or BUILTIN)", t.Kind)
	}

	// Validate timeout is non-negative
	if t.Timeout < 0 {
		return fmt.Errorf("task timeout must be non-negative")
	}

	return nil
}

// SetDefaults sets default values for optional fields
func (c *YamlChain) SetDefaults() {
	// Chain defaults
	if c.Schedule == "" {
		c.Schedule = "* * * * *" // Default to every minute
	}

	// Task defaults
	for i := range c.Tasks {
		task := &c.Tasks[i]
		if task.Kind == "" {
			task.Kind = "SQL"
		}
	}
}

// ParseYamlFile parses a YAML file and returns the configuration
func ParseYamlFile(filePath string) (*YamlConfig, error) {
	// Check if file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return nil, fmt.Errorf("file not found: %s", filePath)
	}

	// Check file extension
	ext := strings.ToLower(filepath.Ext(filePath))
	if ext != ".yaml" && ext != ".yml" {
		return nil, fmt.Errorf("file must have .yaml or .yml extension: %s", filePath)
	}

	// Read file
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	// Parse YAML
	var config YamlConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	// Set defaults and validate
	for i := range config.Chains {
		chain := &config.Chains[i]
		chain.SetDefaults()
		if err := chain.ValidateChain(); err != nil {
			return nil, fmt.Errorf("chain %d (%s): %w", i+1, chain.ChainName, err)
		}
	}

	return &config, nil
}

// ToSQLParameters converts YAML parameters to SQL-compatible format
func (t *YamlTask) ToSQLParameters() (string, error) {
	if len(t.Parameters) == 0 {
		return "", nil
	}

	// Convert to JSON array format for PostgreSQL
	params := make([]string, len(t.Parameters))
	for i, param := range t.Parameters {
		switch v := param.(type) {
		case string:
			params[i] = fmt.Sprintf(`"%s"`, strings.ReplaceAll(v, `"`, `\"`))
		case int, int32, int64:
			params[i] = fmt.Sprintf("%v", v)
		case float32, float64:
			params[i] = fmt.Sprintf("%v", v)
		case bool:
			params[i] = fmt.Sprintf("%t", v)
		default:
			params[i] = fmt.Sprintf(`"%v"`, v)
		}
	}

	return fmt.Sprintf("[%s]", strings.Join(params, ", ")), nil
}
