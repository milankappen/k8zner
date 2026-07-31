//go:build kind

package kind

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestKindSelfHealing tests the self-healing related CRD functionality.
// This test validates that the K8znerCluster CRD properly tracks node health
// and status updates for the self-healing mechanism.
func TestKindSelfHealing(t *testing.T) {
	t.Run("08_SelfHealing", func(t *testing.T) {
		t.Run("StatusPhaseTransitions", testSelfHealingStatusPhaseTransitions)
		t.Run("ConditionUpdates", testSelfHealingConditionUpdates)
		t.Run("UnhealthyNodeTracking", testSelfHealingUnhealthyNodeTracking)
		t.Run("MaxConcurrentHealsEnforcement", testSelfHealingMaxConcurrentHeals)
		t.Run("Cleanup", testSelfHealingCleanup)
	})
}

// testSelfHealingStatusPhaseTransitions tests that status phases can be updated correctly.
func testSelfHealingStatusPhaseTransitions(t *testing.T) {
	// Ensure CRD is installed (depends on operator tests)
	if !fw.IsInstalled("k8zner-crd") {
		t.Log("Installing K8znerCluster CRD...")
		testOperatorCRDInstallation(t)
	}

	// Create test namespace
	fw.KubectlApply(t, `
apiVersion: v1
kind: Namespace
metadata:
  name: k8zner-selfhealing-test
`)

	// Create a test cluster resource
	validCluster := `
apiVersion: k8zner.io/v1alpha1
kind: K8znerCluster
metadata:
  name: self-healing-test
  namespace: k8zner-selfhealing-test
spec:
  region: fsn1
  credentialsRef:
    name: test-credentials
  kubernetes:
    version: 1.34.1
  talos:
    version: v1.12.6
  controlPlanes:
    count: 3
    size: cpx22
  workers:
    count: 2
    size: cpx22
`
	fw.KubectlApply(t, validCluster)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Test phase transitions: Provisioning -> Running
	t.Log("Testing phase transition: Provisioning -> Running")
	pendingPatch := `{"status":{"phase":"Provisioning","controlPlanes":{"total":3,"ready":0,"unhealthy":0},"workers":{"total":2,"ready":0,"unhealthy":0}}}`
	_, err := fw.Kubectl("patch", "k8znercluster", "-n", "k8zner-selfhealing-test", "self-healing-test",
		"--type=merge", "--subresource=status", "-p", pendingPatch)
	if err != nil {
		t.Fatalf("Failed to patch status to Provisioning: %v", err)
	}
	verifyClusterPhase(t, "k8zner-selfhealing-test", "self-healing-test", "Provisioning")

	// Transition to Running
	runningPatch := `{"status":{"phase":"Running","controlPlanes":{"total":3,"ready":3,"unhealthy":0},"workers":{"total":2,"ready":2,"unhealthy":0}}}`
	_, err = fw.Kubectl("patch", "k8znercluster", "-n", "k8zner-selfhealing-test", "self-healing-test",
		"--type=merge", "--subresource=status", "-p", runningPatch)
	if err != nil {
		t.Fatalf("Failed to patch status to Running: %v", err)
	}
	verifyClusterPhase(t, "k8zner-selfhealing-test", "self-healing-test", "Running")

	// Test phase transition: Running -> Degraded
	t.Log("Testing phase transition: Running -> Degraded")
	degradedPatch := `{"status":{"phase":"Degraded","controlPlanes":{"total":3,"ready":2,"unhealthy":1},"workers":{"total":2,"ready":2,"unhealthy":0}}}`
	_, err = fw.Kubectl("patch", "k8znercluster", "-n", "k8zner-selfhealing-test", "self-healing-test",
		"--type=merge", "--subresource=status", "-p", degradedPatch)
	if err != nil {
		t.Fatalf("Failed to patch status to Degraded: %v", err)
	}
	verifyClusterPhase(t, "k8zner-selfhealing-test", "self-healing-test", "Degraded")

	// Test phase transition: Degraded -> Healing
	t.Log("Testing phase transition: Degraded -> Healing")
	healingPatch := `{"status":{"phase":"Healing","controlPlanes":{"total":3,"ready":2,"unhealthy":1},"workers":{"total":2,"ready":2,"unhealthy":0}}}`
	_, err = fw.Kubectl("patch", "k8znercluster", "-n", "k8zner-selfhealing-test", "self-healing-test",
		"--type=merge", "--subresource=status", "-p", healingPatch)
	if err != nil {
		t.Fatalf("Failed to patch status to Healing: %v", err)
	}
	verifyClusterPhase(t, "k8zner-selfhealing-test", "self-healing-test", "Healing")

	// Test phase transition: Healing -> Running (after successful replacement)
	t.Log("Testing phase transition: Healing -> Running")
	_, err = fw.Kubectl("patch", "k8znercluster", "-n", "k8zner-selfhealing-test", "self-healing-test",
		"--type=merge", "--subresource=status", "-p", runningPatch)
	if err != nil {
		t.Fatalf("Failed to patch status back to Running: %v", err)
	}
	verifyClusterPhase(t, "k8zner-selfhealing-test", "self-healing-test", "Running")

	t.Log("✓ Status phase transitions work correctly")
	_ = ctx
}

// testSelfHealingConditionUpdates tests that conditions can be properly tracked.
func testSelfHealingConditionUpdates(t *testing.T) {
	// Update with conditions
	conditionsPatch := `{
		"status": {
			"conditions": [
				{
					"type": "ControlPlaneReady",
					"status": "True",
					"reason": "AllHealthy",
					"message": "All control planes are healthy",
					"lastTransitionTime": "2024-01-01T00:00:00Z"
				},
				{
					"type": "WorkersReady",
					"status": "True",
					"reason": "AllHealthy",
					"message": "All workers are healthy",
					"lastTransitionTime": "2024-01-01T00:00:00Z"
				},
				{
					"type": "Ready",
					"status": "True",
					"reason": "ClusterHealthy",
					"message": "Cluster is fully operational",
					"lastTransitionTime": "2024-01-01T00:00:00Z"
				}
			]
		}
	}`

	_, err := fw.Kubectl("patch", "k8znercluster", "-n", "k8zner-selfhealing-test", "self-healing-test",
		"--type=merge", "--subresource=status", "-p", conditionsPatch)
	if err != nil {
		t.Fatalf("Failed to patch conditions: %v", err)
	}

	// Verify conditions were set
	output, err := fw.Kubectl("get", "k8znercluster", "-n", "k8zner-selfhealing-test", "self-healing-test", "-o", "json")
	if err != nil {
		t.Fatalf("Failed to get cluster: %v", err)
	}

	var cluster map[string]interface{}
	if err := json.Unmarshal([]byte(output), &cluster); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	status := cluster["status"].(map[string]interface{})
	conditions := status["conditions"].([]interface{})

	foundConditions := make(map[string]bool)
	for _, c := range conditions {
		cond := c.(map[string]interface{})
		foundConditions[cond["type"].(string)] = true
	}

	expectedConditions := []string{"ControlPlaneReady", "WorkersReady", "Ready"}
	for _, expected := range expectedConditions {
		if !foundConditions[expected] {
			t.Errorf("Expected condition %s not found", expected)
		}
	}

	// Test QuorumLost condition
	quorumLostPatch := `{
		"status": {
			"phase": "Failed",
			"conditions": [
				{
					"type": "ControlPlaneReady",
					"status": "False",
					"reason": "QuorumLost",
					"message": "Only 1/3 control planes healthy, need 2 for quorum",
					"lastTransitionTime": "2024-01-01T00:00:00Z"
				}
			]
		}
	}`

	_, err = fw.Kubectl("patch", "k8znercluster", "-n", "k8zner-selfhealing-test", "self-healing-test",
		"--type=merge", "--subresource=status", "-p", quorumLostPatch)
	if err != nil {
		t.Fatalf("Failed to patch QuorumLost condition: %v", err)
	}

	// Verify QuorumLost condition
	output, err = fw.Kubectl("get", "k8znercluster", "-n", "k8zner-selfhealing-test", "self-healing-test", "-o", "json")
	if err != nil {
		t.Fatalf("Failed to get cluster: %v", err)
	}

	if err := json.Unmarshal([]byte(output), &cluster); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	status = cluster["status"].(map[string]interface{})
	conditions = status["conditions"].([]interface{})

	foundQuorumLost := false
	for _, c := range conditions {
		cond := c.(map[string]interface{})
		if cond["type"] == "ControlPlaneReady" && cond["reason"] == "QuorumLost" {
			foundQuorumLost = true
			if cond["status"] != "False" {
				t.Errorf("QuorumLost condition should have status False")
			}
			break
		}
	}

	if !foundQuorumLost {
		t.Error("QuorumLost condition not found")
	}

	t.Log("✓ Condition updates work correctly")
}

// testSelfHealingUnhealthyNodeTracking tests tracking of unhealthy nodes in status.
func testSelfHealingUnhealthyNodeTracking(t *testing.T) {
	// Create a second cluster specifically for node tracking tests
	nodeTrackingCluster := `
apiVersion: k8zner.io/v1alpha1
kind: K8znerCluster
metadata:
  name: node-tracking-test
  namespace: k8zner-selfhealing-test
spec:
  region: nbg1
  credentialsRef:
    name: test-credentials
  kubernetes:
    version: 1.34.1
  talos:
    version: v1.12.6
  controlPlanes:
    count: 3
    size: cpx22
  workers:
    count: 3
    size: cpx22
`
	fw.KubectlApply(t, nodeTrackingCluster)

	// Status with detailed node information including unhealthy nodes
	// Note: The CRD schema may not include full node details, but we test what's available
	nodeStatusPatch := `{
		"status": {
			"phase": "Degraded",
			"controlPlanes": {
				"total": 3,
				"ready": 2,
				"unhealthy": 1
			},
			"workers": {
				"total": 3,
				"ready": 2,
				"unhealthy": 1
			}
		}
	}`

	_, err := fw.Kubectl("patch", "k8znercluster", "-n", "k8zner-selfhealing-test", "node-tracking-test",
		"--type=merge", "--subresource=status", "-p", nodeStatusPatch)
	if err != nil {
		t.Fatalf("Failed to patch node status: %v", err)
	}

	// Verify the counts are tracked correctly
	output, err := fw.Kubectl("get", "k8znercluster", "-n", "k8zner-selfhealing-test", "node-tracking-test", "-o", "json")
	if err != nil {
		t.Fatalf("Failed to get cluster: %v", err)
	}

	var cluster map[string]interface{}
	if err := json.Unmarshal([]byte(output), &cluster); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	status := cluster["status"].(map[string]interface{})

	// Verify control plane counts
	cp := status["controlPlanes"].(map[string]interface{})
	if int(cp["ready"].(float64)) != 2 {
		t.Errorf("Expected 2 ready CPs, got %v", cp["ready"])
	}
	if int(cp["unhealthy"].(float64)) != 1 {
		t.Errorf("Expected 1 unhealthy CP, got %v", cp["unhealthy"])
	}

	// Verify worker counts
	workers := status["workers"].(map[string]interface{})
	if int(workers["ready"].(float64)) != 2 {
		t.Errorf("Expected 2 ready workers, got %v", workers["ready"])
	}
	if int(workers["unhealthy"].(float64)) != 1 {
		t.Errorf("Expected 1 unhealthy worker, got %v", workers["unhealthy"])
	}

	// Verify phase is Degraded
	if status["phase"] != "Degraded" {
		t.Errorf("Expected phase Degraded, got %v", status["phase"])
	}

	t.Log("✓ Unhealthy node tracking works correctly")
}

// testSelfHealingMaxConcurrentHeals tests that max concurrent heals can be tracked.
func testSelfHealingMaxConcurrentHeals(t *testing.T) {
	// This test verifies that the status can track multiple unhealthy nodes
	// and that the controller would respect maxConcurrentHeals (tested in unit tests)

	// Create a cluster with multiple unhealthy workers
	multiUnhealthyPatch := `{
		"status": {
			"phase": "Healing",
			"controlPlanes": {
				"total": 3,
				"ready": 3,
				"unhealthy": 0
			},
			"workers": {
				"total": 5,
				"ready": 2,
				"unhealthy": 3
			}
		}
	}`

	_, err := fw.Kubectl("patch", "k8znercluster", "-n", "k8zner-selfhealing-test", "node-tracking-test",
		"--type=merge", "--subresource=status", "-p", multiUnhealthyPatch)
	if err != nil {
		t.Fatalf("Failed to patch multi-unhealthy status: %v", err)
	}

	// Verify the counts
	output, err := fw.Kubectl("get", "k8znercluster", "-n", "k8zner-selfhealing-test", "node-tracking-test", "-o", "json")
	if err != nil {
		t.Fatalf("Failed to get cluster: %v", err)
	}

	var cluster map[string]interface{}
	if err := json.Unmarshal([]byte(output), &cluster); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	status := cluster["status"].(map[string]interface{})
	workers := status["workers"].(map[string]interface{})

	if int(workers["unhealthy"].(float64)) != 3 {
		t.Errorf("Expected 3 unhealthy workers, got %v", workers["unhealthy"])
	}

	// Verify phase is Healing (indicating active replacement)
	if status["phase"] != "Healing" {
		t.Errorf("Expected phase Healing, got %v", status["phase"])
	}

	t.Log("✓ Max concurrent heals tracking verified (unit tests validate enforcement)")
}

// testSelfHealingCleanup cleans up test resources.
func testSelfHealingCleanup(t *testing.T) {
	// Delete test clusters
	_ = fw.KubectlDelete("k8zner-selfhealing-test", "k8znercluster", "self-healing-test")
	_ = fw.KubectlDelete("k8zner-selfhealing-test", "k8znercluster", "node-tracking-test")

	// Delete test namespace
	_ = fw.KubectlDelete("", "namespace", "k8zner-selfhealing-test")

	t.Log("✓ Cleanup completed")
}

// verifyClusterPhase verifies the cluster is in the expected phase.
func verifyClusterPhase(t *testing.T, namespace, name, expectedPhase string) {
	output, err := fw.Kubectl("get", "k8znercluster", "-n", namespace, name, "-o", "jsonpath={.status.phase}")
	if err != nil {
		t.Fatalf("Failed to get cluster phase: %v", err)
	}

	if strings.TrimSpace(output) != expectedPhase {
		t.Errorf("Expected phase %s, got %s", expectedPhase, output)
	}
}
