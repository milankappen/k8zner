package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRoot(t *testing.T) {
	cmd := Root()

	require.NotNil(t, cmd)
	assert.Equal(t, "k8zner", cmd.Use)
	assert.Equal(t, "Provision Kubernetes on Hetzner Cloud using Talos", cmd.Short)
}

func TestRoot_HasSubcommands(t *testing.T) {
	cmd := Root()

	expectedSubcommands := []string{
		"init",
		"apply",
		"destroy",
		"doctor",
		"cost",
		"secrets",
		"kubeconfig",
		"version",
		"completion",
	}

	subcommands := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		subcommands[sub.Name()] = true
	}

	for _, expected := range expectedSubcommands {
		assert.True(t, subcommands[expected], "Expected subcommand %s not found", expected)
	}
}

func TestRoot_SubcommandCount(t *testing.T) {
	cmd := Root()
	assert.Len(t, cmd.Commands(), 9, "Expected 9 subcommands")
}
