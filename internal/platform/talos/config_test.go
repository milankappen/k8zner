package talos

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v3"
)

func TestGenerateControlPlaneConfig(t *testing.T) {
	t.Parallel()
	clusterName := "test-cluster"
	k8sVersion := "v1.30.0"
	talosVersion := "v1.7.0"
	endpoint := "https://1.2.3.4:6443"

	sb, err := NewSecrets(talosVersion)
	assert.NoError(t, err)

	gen := NewGenerator(clusterName, k8sVersion, talosVersion, endpoint, sb)
	assert.NotNil(t, gen)

	san := []string{"api.test-cluster.com"}
	hostname := "test-control-plane-1"
	serverID := int64(12345)
	configBytes, err := gen.GenerateControlPlaneConfig(san, hostname, serverID)
	assert.NoError(t, err)
	assert.NotEmpty(t, configBytes)

	// Validate it is valid YAML
	var result map[string]interface{}
	err = yaml.Unmarshal(configBytes, &result)
	assert.NoError(t, err)

	// Check basic fields
	machine := result["machine"].(map[string]interface{})
	assert.Equal(t, "controlplane", machine["type"])

	// Check patches
	cluster := result["cluster"].(map[string]interface{})
	assert.Equal(t, true, cluster["externalCloudProvider"].(map[string]interface{})["enabled"])

	network := machine["network"].(map[string]interface{})
	assert.Equal(t, hostname, network["hostname"])
}

func TestGenerateWorkerConfig(t *testing.T) {
	t.Parallel()
	clusterName := "test-cluster"
	k8sVersion := "v1.30.0"
	talosVersion := "v1.7.0"
	endpoint := "https://1.2.3.4:6443"

	sb, err := NewSecrets(talosVersion)
	assert.NoError(t, err)

	gen := NewGenerator(clusterName, k8sVersion, talosVersion, endpoint, sb)
	assert.NotNil(t, gen)

	hostname := "test-worker-1"
	serverID := int64(67890)
	configBytes, err := gen.GenerateWorkerConfig(hostname, serverID)
	assert.NoError(t, err)
	assert.NotEmpty(t, configBytes)

	// Validate it is valid YAML
	var result map[string]interface{}
	err = yaml.Unmarshal(configBytes, &result)
	assert.NoError(t, err)

	// Check basic fields
	machine := result["machine"].(map[string]interface{})
	assert.Equal(t, "worker", machine["type"])

	// Check patches
	network := machine["network"].(map[string]interface{})
	assert.Equal(t, hostname, network["hostname"])
}

func TestSecrets(t *testing.T) {
	t.Parallel()
	talosVersion := "v1.7.0"
	tempDir := t.TempDir()
	secretsFile := filepath.Join(tempDir, "secrets.yaml")

	// 1. NewSecrets
	sb, err := NewSecrets(talosVersion)
	assert.NoError(t, err)
	assert.NotNil(t, sb)

	// 2. SaveSecrets
	err = SaveSecrets(secretsFile, sb)
	assert.NoError(t, err)

	// 3. LoadSecrets
	loaded, err := LoadSecrets(secretsFile)
	assert.NoError(t, err)
	assert.NotNil(t, loaded)

	// Verify Load/Save works by checking if LoadBundle succeeds after SaveBundle
	assert.NotNil(t, loaded)
	// Clock is reset in LoadSecrets, so we can check if it's set
	assert.NotNil(t, loaded.Clock)
}

func TestDeepMerge(t *testing.T) {
	t.Parallel()
	t.Run("simple merge", func(t *testing.T) {
		t.Parallel()
		dst := map[string]any{"a": 1, "b": 2}
		src := map[string]any{"b": 3, "c": 4}
		deepMerge(dst, src)
		assert.Equal(t, 1, dst["a"])
		assert.Equal(t, 3, dst["b"])
		assert.Equal(t, 4, dst["c"])
	})

	t.Run("nested merge", func(t *testing.T) {
		t.Parallel()
		dst := map[string]any{
			"machine": map[string]any{
				"type":     "controlplane",
				"hostname": "node1",
			},
		}
		src := map[string]any{
			"machine": map[string]any{
				"hostname": "node2",
				"install": map[string]any{
					"disk": "/dev/sda",
				},
			},
		}
		deepMerge(dst, src)

		machine := dst["machine"].(map[string]any)
		assert.Equal(t, "controlplane", machine["type"])
		assert.Equal(t, "node2", machine["hostname"])
		assert.NotNil(t, machine["install"])
	})

	t.Run("deeply nested merge", func(t *testing.T) {
		t.Parallel()
		dst := map[string]any{
			"level1": map[string]any{
				"level2": map[string]any{
					"level3": map[string]any{
						"keep": "original",
					},
				},
			},
		}
		src := map[string]any{
			"level1": map[string]any{
				"level2": map[string]any{
					"level3": map[string]any{
						"add": "new",
					},
				},
			},
		}
		deepMerge(dst, src)

		l3 := dst["level1"].(map[string]any)["level2"].(map[string]any)["level3"].(map[string]any)
		assert.Equal(t, "original", l3["keep"])
		assert.Equal(t, "new", l3["add"])
	})

	t.Run("non-map overwrites map", func(t *testing.T) {
		t.Parallel()
		dst := map[string]any{
			"config": map[string]any{"key": "value"},
		}
		src := map[string]any{
			"config": "simple-string",
		}
		deepMerge(dst, src)
		assert.Equal(t, "simple-string", dst["config"])
	})

	t.Run("map overwrites non-map", func(t *testing.T) {
		t.Parallel()
		dst := map[string]any{
			"config": "simple-string",
		}
		src := map[string]any{
			"config": map[string]any{"key": "value"},
		}
		deepMerge(dst, src)
		assert.Equal(t, map[string]any{"key": "value"}, dst["config"])
	})

	t.Run("empty source", func(t *testing.T) {
		t.Parallel()
		dst := map[string]any{"a": 1}
		src := map[string]any{}
		deepMerge(dst, src)
		assert.Equal(t, 1, dst["a"])
	})

	t.Run("empty destination", func(t *testing.T) {
		t.Parallel()
		dst := map[string]any{}
		src := map[string]any{"a": 1}
		deepMerge(dst, src)
		assert.Equal(t, 1, dst["a"])
	})
}

func TestStripComments(t *testing.T) {
	t.Parallel()
	t.Run("removes comment lines", func(t *testing.T) {
		t.Parallel()
		input := []byte("# comment\nkey: value\n# another comment\nother: data")
		result := stripComments(input)
		assert.NotContains(t, string(result), "# comment")
		assert.NotContains(t, string(result), "# another comment")
		assert.Contains(t, string(result), "key: value")
		assert.Contains(t, string(result), "other: data")
	})

	t.Run("removes empty lines", func(t *testing.T) {
		t.Parallel()
		input := []byte("key: value\n\n\nother: data")
		result := stripComments(input)
		lines := len(splitLines(string(result)))
		assert.Equal(t, 2, lines)
	})

	t.Run("preserves indented content", func(t *testing.T) {
		t.Parallel()
		input := []byte("root:\n  nested: value\n  # comment\n  other: data")
		result := stripComments(input)
		assert.Contains(t, string(result), "  nested: value")
		assert.Contains(t, string(result), "  other: data")
		assert.NotContains(t, string(result), "# comment")
	})

	t.Run("handles indented comments", func(t *testing.T) {
		t.Parallel()
		input := []byte("root:\n    # indented comment\n  key: value")
		result := stripComments(input)
		assert.NotContains(t, string(result), "# indented comment")
		assert.Contains(t, string(result), "key: value")
	})

	t.Run("empty input", func(t *testing.T) {
		t.Parallel()
		result := stripComments([]byte(""))
		assert.Empty(t, result)
	})

	t.Run("only comments", func(t *testing.T) {
		t.Parallel()
		input := []byte("# comment 1\n# comment 2\n   # comment 3")
		result := stripComments(input)
		assert.Empty(t, result)
	})
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	var lines []string
	for _, line := range splitByNewline(s) {
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func splitByNewline(s string) []string {
	return append([]string{}, splitString(s, "\n")...)
}

func splitString(s, sep string) []string {
	var result []string
	start := 0
	for i := 0; i <= len(s)-len(sep); i++ {
		if s[i:i+len(sep)] == sep {
			result = append(result, s[start:i])
			start = i + len(sep)
		}
	}
	result = append(result, s[start:])
	return result
}

func TestApplyConfigPatch(t *testing.T) {
	t.Parallel()
	t.Run("applies simple patch", func(t *testing.T) {
		t.Parallel()
		baseConfig := []byte("machine:\n  type: worker\n")
		patch := map[string]any{
			"machine": map[string]any{
				"hostname": "test-node",
			},
		}
		result, err := applyConfigPatch(baseConfig, patch)
		assert.NoError(t, err)

		var config map[string]any
		err = yaml.Unmarshal(result, &config)
		assert.NoError(t, err)

		machine := config["machine"].(map[string]any)
		assert.Equal(t, "worker", machine["type"])
		assert.Equal(t, "test-node", machine["hostname"])
	})

	t.Run("deep merges nested structures", func(t *testing.T) {
		t.Parallel()
		baseConfig := []byte("machine:\n  network:\n    hostname: original\n    interfaces: []\n")
		patch := map[string]any{
			"machine": map[string]any{
				"network": map[string]any{
					"hostname":    "patched",
					"nameservers": []string{"8.8.8.8"},
				},
			},
		}
		result, err := applyConfigPatch(baseConfig, patch)
		assert.NoError(t, err)

		var config map[string]any
		err = yaml.Unmarshal(result, &config)
		assert.NoError(t, err)

		network := config["machine"].(map[string]any)["network"].(map[string]any)
		assert.Equal(t, "patched", network["hostname"])
		assert.NotNil(t, network["interfaces"])
		assert.NotNil(t, network["nameservers"])
	})

	t.Run("handles invalid YAML", func(t *testing.T) {
		t.Parallel()
		baseConfig := []byte("invalid: yaml: content: [")
		patch := map[string]any{"key": "value"}
		_, err := applyConfigPatch(baseConfig, patch)
		assert.Error(t, err)
	})
}

func TestGetInstallerImageURL(t *testing.T) {
	t.Parallel()
	t.Run("default installer without schematic", func(t *testing.T) {
		t.Parallel()
		gen := &Generator{
			talosVersion: "v1.7.0",
			machineOpts:  &MachineConfigOptions{},
		}
		url := gen.getInstallerImageURL()
		assert.Equal(t, "ghcr.io/siderolabs/installer:v1.7.0", url)
	})

	t.Run("factory installer with schematic", func(t *testing.T) {
		t.Parallel()
		gen := &Generator{
			talosVersion: "v1.7.0",
			machineOpts: &MachineConfigOptions{
				SchematicID: "abc123",
			},
		}
		url := gen.getInstallerImageURL()
		assert.Equal(t, "factory.talos.dev/installer/abc123:v1.7.0", url)
	})

	t.Run("nil machine opts", func(t *testing.T) {
		t.Parallel()
		gen := &Generator{
			talosVersion: "v1.7.0",
			machineOpts:  nil,
		}
		url := gen.getInstallerImageURL()
		assert.Equal(t, "ghcr.io/siderolabs/installer:v1.7.0", url)
	})
}

func TestNewGenerator(t *testing.T) {
	t.Parallel()
	t.Run("strips v prefix from kubernetes version", func(t *testing.T) {
		t.Parallel()
		sb, _ := NewSecrets("v1.7.0")
		gen := NewGenerator("test", "v1.30.0", "v1.7.0", "https://endpoint", sb)
		assert.Equal(t, "1.30.0", gen.kubernetesVersion)
	})

	t.Run("handles version without v prefix", func(t *testing.T) {
		t.Parallel()
		sb, _ := NewSecrets("v1.7.0")
		gen := NewGenerator("test", "1.30.0", "v1.7.0", "https://endpoint", sb)
		assert.Equal(t, "1.30.0", gen.kubernetesVersion)
	})

	t.Run("initializes with empty machine opts", func(t *testing.T) {
		t.Parallel()
		sb, _ := NewSecrets("v1.7.0")
		gen := NewGenerator("test", "v1.30.0", "v1.7.0", "https://endpoint", sb)
		assert.NotNil(t, gen.machineOpts)
	})
}

func TestSetMachineConfigOptions(t *testing.T) {
	t.Parallel()
	t.Run("sets options from valid pointer", func(t *testing.T) {
		t.Parallel()
		sb, _ := NewSecrets("v1.7.0")
		gen := NewGenerator("test", "v1.30.0", "v1.7.0", "https://endpoint", sb)

		opts := &MachineConfigOptions{
			SchematicID: "test-schematic",
		}
		gen.SetMachineConfigOptions(opts)
		assert.Equal(t, "test-schematic", gen.machineOpts.SchematicID)
	})

	t.Run("ignores nil options", func(t *testing.T) {
		t.Parallel()
		sb, _ := NewSecrets("v1.7.0")
		gen := NewGenerator("test", "v1.30.0", "v1.7.0", "https://endpoint", sb)
		original := gen.machineOpts

		gen.SetMachineConfigOptions(nil)
		assert.Equal(t, original, gen.machineOpts)
	})

	t.Run("ignores non-MachineConfigOptions type", func(t *testing.T) {
		t.Parallel()
		sb, _ := NewSecrets("v1.7.0")
		gen := NewGenerator("test", "v1.30.0", "v1.7.0", "https://endpoint", sb)
		original := gen.machineOpts

		gen.SetMachineConfigOptions("invalid type")
		assert.Equal(t, original, gen.machineOpts)
	})
}

func TestSetEndpoint(t *testing.T) {
	t.Parallel()
	sb, _ := NewSecrets("v1.7.0")
	gen := NewGenerator("test", "v1.30.0", "v1.7.0", "https://original", sb)
	assert.Equal(t, "https://original", gen.endpoint)

	gen.SetEndpoint("https://new-endpoint")
	assert.Equal(t, "https://new-endpoint", gen.endpoint)
}

func TestGetOrGenerateSecrets(t *testing.T) {
	t.Parallel()
	t.Run("generates new secrets when file doesn't exist", func(t *testing.T) {
		t.Parallel()
		tempDir := t.TempDir()
		secretsFile := filepath.Join(tempDir, "new-secrets.yaml")

		sb, err := GetOrGenerateSecrets(secretsFile, "v1.7.0")
		assert.NoError(t, err)
		assert.NotNil(t, sb)
	})

	t.Run("loads existing secrets", func(t *testing.T) {
		t.Parallel()
		tempDir := t.TempDir()
		secretsFile := filepath.Join(tempDir, "secrets.yaml")

		// First create secrets
		sb1, err := NewSecrets("v1.7.0")
		assert.NoError(t, err)
		err = SaveSecrets(secretsFile, sb1)
		assert.NoError(t, err)

		// Then load them
		sb2, err := GetOrGenerateSecrets(secretsFile, "v1.7.0")
		assert.NoError(t, err)
		assert.NotNil(t, sb2)
	})
}

func TestLoadSecrets_Errors(t *testing.T) {
	t.Parallel()
	t.Run("returns error for non-existent file", func(t *testing.T) {
		t.Parallel()
		_, err := LoadSecrets("/nonexistent/path/secrets.yaml")
		assert.Error(t, err)
	})

	t.Run("returns error for invalid file", func(t *testing.T) {
		t.Parallel()
		tempDir := t.TempDir()
		invalidFile := filepath.Join(tempDir, "invalid.yaml")
		err := writeTestFile(invalidFile, "not valid yaml: [")
		assert.NoError(t, err)

		_, err = LoadSecrets(invalidFile)
		assert.Error(t, err)
	})
}

func writeTestFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0600)
}

// TestGeneratedConfigEncryptsSecretsAtRest pins a security property we rely
// on: Talos-generated control plane configs must carry a secretbox encryption
// secret so Kubernetes Secrets are encrypted at rest in etcd. The machinery
// includes it by default; this test ensures a future generator change or
// machinery upgrade cannot silently drop it.
func TestGeneratedConfigEncryptsSecretsAtRest(t *testing.T) {
	t.Parallel()

	sb, err := NewSecrets("v1.9.0")
	if err != nil {
		t.Fatal(err)
	}

	g := NewGenerator("enc-check", "1.32.0", "v1.9.0", "https://1.2.3.4:6443", sb)
	cfg, err := g.GenerateControlPlaneConfig([]string{"1.2.3.4"}, "cp-1", 1)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(cfg), "secretboxEncryptionSecret") {
		t.Error("control plane config lacks secretboxEncryptionSecret: Kubernetes Secrets would be stored unencrypted in etcd")
	}
}
