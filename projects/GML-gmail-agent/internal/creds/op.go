package creds

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
)

const (
	OPItemReadOnly  = "GML Gmail Read-Only Credentials"
	OPItemReadWrite = "GML Gmail Read-Write Credentials"
	OPField         = "credential"
)

func LoadFromOP(itemName, fieldName string) (*Creds, error) {
	cmd := exec.Command("op", "item", "get", itemName, "--fields", fieldName, "--reveal", "--format", "json")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("op item get %q: %w\n%s", itemName, err, stderr.String())
	}

	var field struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &field); err != nil {
		return nil, fmt.Errorf("parsing op output: %w", err)
	}
	if field.Value == "" {
		return nil, fmt.Errorf("op returned empty value for %s/%s", itemName, fieldName)
	}

	return Load(bytes.NewReader([]byte(field.Value)))
}
