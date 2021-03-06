package e2e

import (
	goctx "context"
	"fmt"
	mcfgv1 "github.com/openshift/compliance-operator/pkg/apis/machineconfiguration/v1"
	mcfgClient "github.com/openshift/compliance-operator/pkg/generated/clientset/versioned/typed/machineconfiguration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"testing"
	"time"

	framework "github.com/operator-framework/operator-sdk/pkg/test"
	"github.com/operator-framework/operator-sdk/pkg/test/e2eutil"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/compliance-operator/pkg/apis"
	complianceoperatorv1alpha1 "github.com/openshift/compliance-operator/pkg/apis/complianceoperator/v1alpha1"
)

type testExecution struct {
	Name   string
	TestFn func(*testing.T, *framework.Framework, *framework.TestCtx, string) error
}

// executeTest sets up everything that a e2e test needs to run, and executes the test.
func executeTests(t *testing.T, tests ...testExecution) {
	ctx := setupTestRequirements(t)
	defer ctx.Cleanup()

	setupComplianceOperatorCluster(t, ctx)

	// get global framework variables
	f := framework.Global

	ns, err := ctx.GetNamespace()
	if err != nil {
		t.Fatalf("could not get namespace: %v", err)
	}

	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			if err := test.TestFn(t, f, ctx, ns); err != nil {
				t.Error(err)
			}
		})

	}
}

// setupTestRequirements Adds the items to the client's schema (So we can use our objects in the client)
// and creates a test context.
//
// NOTE: Whenever we add new types to the operator, we need to register them here for the e2e tests.
func setupTestRequirements(t *testing.T) *framework.TestCtx {
	objects := [3]runtime.Object{&complianceoperatorv1alpha1.ComplianceScanList{},
								 &complianceoperatorv1alpha1.ComplianceRemediationList{},
							     &complianceoperatorv1alpha1.ComplianceSuiteList{}}
	for _, obj := range objects {
		err := framework.AddToFrameworkScheme(apis.AddToScheme, obj)
		if err != nil {
			t.Fatalf("TEST SETUP: failed to add custom resource scheme to framework: %v", err)
		}
	}
	return framework.NewTestCtx(t)
}

// setupComplianceOperatorCluster creates a compliance-operator cluster and the resources it needs to operate
// such as the namespace, permissions, etc.
func setupComplianceOperatorCluster(t *testing.T, ctx *framework.TestCtx) {
	err := ctx.InitializeClusterResources(&framework.CleanupOptions{TestContext: ctx, Timeout: cleanupTimeout, RetryInterval: cleanupRetryInterval})
	if err != nil {
		t.Fatalf("failed to initialize cluster resources: %v", err)
	}
	t.Log("Initialized cluster resources")
	namespace, err := ctx.GetNamespace()
	if err != nil {
		t.Fatal(err)
	}
	// get global framework variables
	f := framework.Global
	// wait for compliance-operator to be ready
	err = e2eutil.WaitForOperatorDeployment(t, f.KubeClient, namespace, "compliance-operator", 1, retryInterval, timeout)
	if err != nil {
		t.Fatal(err)
	}
}

// waitForScanStatus will poll until the compliancescan that we're lookingfor reaches a certain status, or until
// a timeout is reached.
func waitForScanStatus(t *testing.T, f *framework.Framework, namespace, name string, targetStatus complianceoperatorv1alpha1.ComplianceScanStatusPhase) error {
	exampleComplianceScan := &complianceoperatorv1alpha1.ComplianceScan{}
	var lastErr error
	// retry and ignore errors until timeout
	timeouterr := wait.Poll(retryInterval, timeout, func() (bool, error) {
		lastErr = f.Client.Get(goctx.TODO(), types.NamespacedName{Name: name, Namespace: namespace}, exampleComplianceScan)
		if lastErr != nil {
			if apierrors.IsNotFound(lastErr) {
				t.Logf("Waiting for availability of %s compliancescan\n", name)
				return false, nil
			}
			t.Logf("Retrying. Got error: %v\n", lastErr)
			return false, nil
		}

		if exampleComplianceScan.Status.Phase == targetStatus {
			return true, nil
		}
		t.Logf("Waiting for run of %s compliancescan (%s)\n", name, exampleComplianceScan.Status.Phase)
		return false, nil
	})
	// Error in function call
	if lastErr != nil {
		return lastErr
	}
	// Timeout
	if timeouterr != nil {
		return timeouterr
	}
	t.Logf("ComplianceScan ready (%s)\n", exampleComplianceScan.Status.Phase)
	return nil
}

// waitForScanStatus will poll until the compliancescan that we're lookingfor reaches a certain status, or until
// a timeout is reached.
func waitForSuiteScansStatus(t *testing.T, f *framework.Framework, namespace, name string, targetStatus complianceoperatorv1alpha1.ComplianceScanStatusPhase) error {
	suite := &complianceoperatorv1alpha1.ComplianceSuite{}
	var lastErr error
	// retry and ignore errors until timeout
	timeouterr := wait.Poll(retryInterval, timeout, func() (bool, error) {
		lastErr = f.Client.Get(goctx.TODO(), types.NamespacedName{Name: name, Namespace: namespace}, suite)
		if lastErr != nil {
			if apierrors.IsNotFound(lastErr) {
				t.Logf("Waiting for availability of %s compliancesuite\n", name)
				return false, nil
			}
			t.Logf("Retrying. Got error: %v\n", lastErr)
			return false, nil
		}

		// Got the suite. There should be at least one scan or else we're still initialising
		if len(suite.Status.ScanStatuses) < 1 {
			return false, nil
		}

		//Examine the scan status both in the suite status and the scan
		for _, scanStatus := range suite.Status.ScanStatuses {
			if scanStatus.Phase != targetStatus {
				t.Logf("Waiting until scan %s is done", scanStatus.Name)
				return false, nil
			}

			lastErr = waitForScanStatus(t, f, namespace, scanStatus.Name, targetStatus)
			if lastErr != nil {
				// If the status was present in the suite, then /any/ error
				// should fail the test as the scans should be read /from/
				// the scan itself
				return true, nil
			}
		}

		return true, nil
	})

	// Error in function call
	if lastErr != nil {
		return lastErr
	}

	// Timeout
	if timeouterr != nil {
		return timeouterr
	}

	t.Logf("All scans in ComplianceSuite have finished (%s)\n", suite.Name)
	return nil
}

func scanResultIsExpected(f *framework.Framework, namespace, name string, expectedResult complianceoperatorv1alpha1.ComplianceScanStatusResult) error {
	cs := &complianceoperatorv1alpha1.ComplianceScan{}
	err := f.Client.Get(goctx.TODO(), types.NamespacedName{Name: name, Namespace: namespace}, cs)
	if err != nil {
		return err
	}
	if cs.Status.Result != expectedResult {
		return fmt.Errorf("The ComplianceScan Result wasn't what we expected. Got '%s', expected '%s'", cs.Status.Result, expectedResult)
	}
	return nil
}

// getNodesWithSelector lists nodes according to a specific selector
func getNodesWithSelector(f *framework.Framework, labelselector map[string]string) []corev1.Node {
	var nodes corev1.NodeList
	lo := &client.ListOptions{
		LabelSelector: labels.SelectorFromSet(labelselector),
	}
	f.Client.List(goctx.TODO(), &nodes, lo)
	return nodes.Items
}

func getPodsForScan(f *framework.Framework, scanName string) ([]corev1.Pod, error) {
	selectPods := map[string]string{
		"complianceScan": scanName,
	}
	var pods corev1.PodList
	lo := &client.ListOptions{
		LabelSelector: labels.SelectorFromSet(selectPods),
	}
	err := f.Client.List(goctx.TODO(), &pods, lo)
	if err != nil {
		return nil, err
	}
	return pods.Items, nil
}

// getConfigMapsFromScan lists the configmaps from the specified openscap scan instance
func getConfigMapsFromScan(f *framework.Framework, scaninstance *complianceoperatorv1alpha1.ComplianceScan) []corev1.ConfigMap {
	var configmaps corev1.ConfigMapList
	labelselector := map[string]string{
		"compliance-scan": scaninstance.Name,
	}
	lo := &client.ListOptions{
		LabelSelector: labels.SelectorFromSet(labelselector),
	}
	f.Client.List(goctx.TODO(), &configmaps, lo)
	return configmaps.Items
}

func getRemediationsFromScan(f *framework.Framework, suiteName, scanName string) []complianceoperatorv1alpha1.ComplianceRemediation {
	var scanSuiteRemediations complianceoperatorv1alpha1.ComplianceRemediationList

	scanSuiteSelector := make(map[string]string)
	scanSuiteSelector[complianceoperatorv1alpha1.SuiteLabel] = suiteName
	scanSuiteSelector[complianceoperatorv1alpha1.ScanLabel] = scanName

	listOpts := client.ListOptions{
		LabelSelector: labels.SelectorFromSet(scanSuiteSelector),
	}

	f.Client.List(goctx.TODO(), &scanSuiteRemediations, &listOpts)
	return scanSuiteRemediations.Items
}

func assertHasRemediations(f *framework.Framework, suiteName, scanName, roleLabel string, remNameList []string) error {
	var scanSuiteMapNames = make(map[string]bool)

	scanSuiteRemediations := getRemediationsFromScan(f, suiteName, scanName)
	for _, rem := range scanSuiteRemediations {
		scanSuiteMapNames[rem.Name] = true
	}

	for _, expRem := range remNameList {
		_, ok := scanSuiteMapNames[expRem]
		if !ok {
			return fmt.Errorf("expected remediation %s not found", expRem)
		}
	}

	for _, rem := range scanSuiteRemediations {
		if rem.Labels["machineconfiguration.openshift.io/role"] != roleLabel {
			return fmt.Errorf("expected that scan %s is labeled for role %s", scanName, roleLabel)
		}
	}
	return nil
}

type MachineConfigActionFunc func() (error)
type PoolPredicate func(pool *mcfgv1.MachineConfigPool) (bool, error)

func waitForMachinePoolUpdate(t *testing.T, mcClient *mcfgClient.MachineconfigurationV1Client, name string, action MachineConfigActionFunc, predicate PoolPredicate) error {
	poolPre, err := mcClient.MachineConfigPools().Get(name, metav1.GetOptions{})
	if err != nil {
		t.Errorf("Could not find the pool pre update")
		return err
	}
	t.Logf("Pre-update, MC Pool %s has generation %d", poolPre.Name, poolPre.Status.ObservedGeneration)

	err = action()
	if err != nil {
		t.Errorf("Action failed %v", err)
		return err
	}

	// Should we make this configurable? Maybe 5 minutes is not enough time for slower clusters?
	wait.PollImmediate(5*time.Second, 5*time.Minute, func() (bool, error) {
		pool, err := mcClient.MachineConfigPools().Get(name, metav1.GetOptions{})
		if err != nil {
			// even not found is a hard error here
			t.Errorf("Could not find the pool post update")
			return false, err
		}

		ok, err := predicate(pool)
		if err != nil {
			t.Errorf("Predicate failed %v", err)
			return false, err
		}

		if !ok {
			t.Logf("Predicate not true yet, waiting")
			return false, nil
		}

		t.Logf("Will check for update, Gen: %d, previous %d updated %d/%d unavailable %d",
			pool.Status.ObservedGeneration, poolPre.Status.ObservedGeneration,
			pool.Status.UpdatedMachineCount, pool.Status.MachineCount,
			pool.Status.UnavailableMachineCount)

		// Check if the pool has finished updating yet
		if (pool.Status.ObservedGeneration > poolPre.Status.ObservedGeneration) &&
			(pool.Status.UpdatedMachineCount == pool.Status.MachineCount) &&
			(pool.Status.UnavailableMachineCount == 0) {
			t.Logf("The pool has updated")
			return true, nil
		}

		t.Logf("The pool has not updated yet. Gen: %d, expected %d updated %d/%d unavailable %d",
				pool.Status.ObservedGeneration, poolPre.Status.ObservedGeneration,
				pool.Status.UpdatedMachineCount, pool.Status.MachineCount,
				pool.Status.UnavailableMachineCount)
		return false, nil
	})

	return nil
}
