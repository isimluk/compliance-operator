package e2e

import (
	goctx "context"
	"fmt"
	mcfgv1 "github.com/openshift/compliance-operator/pkg/apis/machineconfiguration/v1"
	mcfgClient "github.com/openshift/compliance-operator/pkg/generated/clientset/versioned/typed/machineconfiguration/v1"
	framework "github.com/operator-framework/operator-sdk/pkg/test"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"math/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"testing"

	complianceoperatorv1alpha1 "github.com/openshift/compliance-operator/pkg/apis/complianceoperator/v1alpha1"
)

func TestE2E(t *testing.T) {
	executeTests(t,
		testExecution{
			Name: "TestSingleScanSucceeds",
			TestFn: func(t *testing.T, f *framework.Framework, ctx *framework.TestCtx, namespace string) error {
				exampleComplianceScan := &complianceoperatorv1alpha1.ComplianceScan{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-single-scan",
						Namespace: namespace,
					},
					Spec: complianceoperatorv1alpha1.ComplianceScanSpec{
						Profile: "xccdf_org.ssgproject.content_profile_coreos-ncp",
						Content: "ssg-ocp4-ds.xml",
						Rule:    "xccdf_org.ssgproject.content_rule_no_netrc_files",
					},
				}
				// use TestCtx's create helper to create the object and add a cleanup function for the new object
				err := f.Client.Create(goctx.TODO(), exampleComplianceScan, &framework.CleanupOptions{TestContext: ctx, Timeout: cleanupTimeout, RetryInterval: cleanupRetryInterval})
				if err != nil {
					return err
				}
				err = waitForScanStatus(t, f, namespace, "test-single-scan", complianceoperatorv1alpha1.PhaseDone)
				if err != nil {
					return err
				}

				return scanResultIsExpected(f, namespace, "test-single-scan", complianceoperatorv1alpha1.ResultCompliant)
			},
		},
		testExecution{
			Name: "TestScanWithNodeSelectorFiltersCorrectly",
			TestFn: func(t *testing.T, f *framework.Framework, ctx *framework.TestCtx, namespace string) error {
				selectWorkers := map[string]string{
					"node-role.kubernetes.io/worker": "",
				}
				testComplianceScan := &complianceoperatorv1alpha1.ComplianceScan{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-filtered-scan",
						Namespace: namespace,
					},
					Spec: complianceoperatorv1alpha1.ComplianceScanSpec{
						Profile:      "xccdf_org.ssgproject.content_profile_coreos-ncp",
						Content:      "ssg-ocp4-ds.xml",
						Rule:         "xccdf_org.ssgproject.content_rule_no_netrc_files",
						NodeSelector: selectWorkers,
					},
				}
				// use TestCtx's create helper to create the object and add a cleanup function for the new object
				err := f.Client.Create(goctx.TODO(), testComplianceScan, &framework.CleanupOptions{TestContext: ctx, Timeout: cleanupTimeout, RetryInterval: cleanupRetryInterval})
				if err != nil {
					return err
				}
				err = waitForScanStatus(t, f, namespace, "test-filtered-scan", complianceoperatorv1alpha1.PhaseDone)
				if err != nil {
					return err
				}
				nodes := getNodesWithSelector(f, selectWorkers)
				configmaps := getConfigMapsFromScan(f, testComplianceScan)
				if len(nodes) != len(configmaps) {
					return fmt.Errorf(
						"The number of reports doesn't match the number of selected nodes: "+
							"%d reports / %d nodes", len(configmaps), len(nodes))
				}
				return scanResultIsExpected(f, namespace, "test-filtered-scan", complianceoperatorv1alpha1.ResultCompliant)
			},
		},
		testExecution{
			Name: "TestScanWithInvalidContentFails",
			TestFn: func(t *testing.T, f *framework.Framework, ctx *framework.TestCtx, namespace string) error {
				exampleComplianceScan := &complianceoperatorv1alpha1.ComplianceScan{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-scan-w-invalid-content",
						Namespace: namespace,
					},
					Spec: complianceoperatorv1alpha1.ComplianceScanSpec{
						Profile: "xccdf_org.ssgproject.content_profile_coreos-ncp",
						Content: "ssg-ocp4-non-existent.xml",
					},
				}
				// use TestCtx's create helper to create the object and add a cleanup function for the new object
				err := f.Client.Create(goctx.TODO(), exampleComplianceScan, &framework.CleanupOptions{TestContext: ctx, Timeout: cleanupTimeout, RetryInterval: cleanupRetryInterval})
				if err != nil {
					return err
				}
				err = waitForScanStatus(t, f, namespace, "test-scan-w-invalid-content", complianceoperatorv1alpha1.PhaseDone)
				if err != nil {
					return err
				}
				return scanResultIsExpected(f, namespace, "test-scan-w-invalid-content", complianceoperatorv1alpha1.ResultError)
			},
		},
		testExecution{
			Name: "TestScanWithInvalidProfileFails",
			TestFn: func(t *testing.T, f *framework.Framework, ctx *framework.TestCtx, namespace string) error {
				exampleComplianceScan := &complianceoperatorv1alpha1.ComplianceScan{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-scan-w-invalid-profile",
						Namespace: namespace,
					},
					Spec: complianceoperatorv1alpha1.ComplianceScanSpec{
						Profile: "xccdf_org.ssgproject.content_profile_coreos-unexistent",
						Content: "ssg-ocp4-ds.xml",
					},
				}
				// use TestCtx's create helper to create the object and add a cleanup function for the new object
				err := f.Client.Create(goctx.TODO(), exampleComplianceScan, &framework.CleanupOptions{TestContext: ctx, Timeout: cleanupTimeout, RetryInterval: cleanupRetryInterval})
				if err != nil {
					return err
				}
				err = waitForScanStatus(t, f, namespace, "test-scan-w-invalid-profile", complianceoperatorv1alpha1.PhaseDone)
				if err != nil {
					return err
				}
				return scanResultIsExpected(f, namespace, "test-scan-w-invalid-profile", complianceoperatorv1alpha1.ResultError)
			},
		},
		testExecution{
			Name: "TestMissingPodInRunningState",
			TestFn: func(t *testing.T, f *framework.Framework, ctx *framework.TestCtx, namespace string) error {
				exampleComplianceScan := &complianceoperatorv1alpha1.ComplianceScan{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-missing-pod-scan",
						Namespace: namespace,
					},
					Spec: complianceoperatorv1alpha1.ComplianceScanSpec{
						Profile: "xccdf_org.ssgproject.content_profile_coreos-ncp",
						Content: "ssg-ocp4-ds.xml",
						Rule:    "xccdf_org.ssgproject.content_rule_no_netrc_files",
					},
				}
				// use TestCtx's create helper to create the object and add a cleanup function for the new object
				err := f.Client.Create(goctx.TODO(), exampleComplianceScan, &framework.CleanupOptions{TestContext: ctx, Timeout: cleanupTimeout, RetryInterval: cleanupRetryInterval})
				if err != nil {
					return err
				}
				err = waitForScanStatus(t, f, namespace, "test-missing-pod-scan", complianceoperatorv1alpha1.PhaseRunning)
				if err != nil {
					return err
				}
				pods, err := getPodsForScan(f, "test-missing-pod-scan")
				if err != nil {
					return err
				}
				if len(pods) < 1 {
					return fmt.Errorf("No pods gotten from query for the scan")
				}
				podToDelete := pods[rand.Intn(len(pods))]
				// Delete pod ASAP
				zeroSeconds := int64(0)
				do := client.DeleteOptions{GracePeriodSeconds: &zeroSeconds}
				err = f.Client.Delete(goctx.TODO(), &podToDelete, &do)
				if err != nil {
					return err
				}
				err = waitForScanStatus(t, f, namespace, "test-missing-pod-scan", complianceoperatorv1alpha1.PhaseDone)
				if err != nil {
					return err
				}
				return scanResultIsExpected(f, namespace, "test-missing-pod-scan", complianceoperatorv1alpha1.ResultCompliant)
			},
		},
		testExecution{
			Name: "TestSuiteScan",
			TestFn: func(t *testing.T, f *framework.Framework, ctx *framework.TestCtx, namespace string) error {
				suiteName := "test-suite-two-scans"

				workerScanName := fmt.Sprintf("%s-workers-scan", suiteName)
				selectWorkers := map[string]string{
					"node-role.kubernetes.io/worker": "",
				}

				masterScanName := fmt.Sprintf("%s-masters-scan", suiteName)
				selectMasters := map[string]string{
					"node-role.kubernetes.io/master": "",
				}

				exampleComplianceSuite := &complianceoperatorv1alpha1.ComplianceSuite{
					ObjectMeta: metav1.ObjectMeta{
						Name:      suiteName,
						Namespace: namespace,
					},
					Spec: complianceoperatorv1alpha1.ComplianceSuiteSpec{
						AutoApplyRemediations: false,
						Scans: []complianceoperatorv1alpha1.ComplianceScanSpecWrapper{
							{
								ComplianceScanSpec: complianceoperatorv1alpha1.ComplianceScanSpec{
									ContentImage: "quay.io/jhrozek/ocp4-openscap-content:remediation_demo",
									Profile:      "xccdf_org.ssgproject.content_profile_coreos-ncp",
									Content:      "ssg-ocp4-ds.xml",
									NodeSelector: selectWorkers,
								},
								Name: workerScanName,
							},
							{
								ComplianceScanSpec: complianceoperatorv1alpha1.ComplianceScanSpec{
									ContentImage: "quay.io/jhrozek/ocp4-openscap-content:remediation_demo",
									Profile:      "xccdf_org.ssgproject.content_profile_coreos-ncp",
									Content:      "ssg-ocp4-ds.xml",
									NodeSelector: selectMasters,
								},
								Name: masterScanName,
							},
						},
					},
				}

				err := f.Client.Create(goctx.TODO(), exampleComplianceSuite, &framework.CleanupOptions{TestContext: ctx, Timeout: cleanupTimeout, RetryInterval: cleanupRetryInterval})
				if err != nil {
					return err
				}

				// Ensure that all the scans in the suite have finished and are marked as Done
				err = waitForSuiteScansStatus(t, f, namespace, suiteName, complianceoperatorv1alpha1.PhaseDone)
				if err != nil {
					return err
				}

				// At this point, both scans should be non-compliant given our current content
				err = scanResultIsExpected(f, namespace, workerScanName, complianceoperatorv1alpha1.ResultNonCompliant)
				if err != nil {
					return err
				}
				err = scanResultIsExpected(f, namespace, masterScanName, complianceoperatorv1alpha1.ResultNonCompliant)
				if err != nil {
					return err
				}

				// Each scan should produce two remediations
				workerRemediations := []string{
					fmt.Sprintf("%s-no-empty-passwords", workerScanName),
					fmt.Sprintf("%s-no-direct-root-logins", workerScanName),
				}
				err = assertHasRemediations(f, suiteName, workerScanName, "worker", workerRemediations)
				if err != nil {
					return err
				}

				masterRemediations := []string{
					fmt.Sprintf("%s-no-empty-passwords", masterScanName),
					fmt.Sprintf("%s-no-direct-root-logins", masterScanName),
				}
				err = assertHasRemediations(f, suiteName, masterScanName, "master", masterRemediations)
				if err != nil {
					return err
				}
				return nil
			},
		},
		testExecution{
			Name: "TestRemediate",
			TestFn: func(t *testing.T, f *framework.Framework, ctx *framework.TestCtx, namespace string) error {
				// FIXME, maybe have a func that returns a struct with suite name and scan names?
				suiteName := "test-remediate"

				workerScanName := fmt.Sprintf("%s-workers-scan", suiteName)
				selectWorkers := map[string]string{
					"node-role.kubernetes.io/worker": "",
				}

				exampleComplianceSuite := &complianceoperatorv1alpha1.ComplianceSuite{
					ObjectMeta: metav1.ObjectMeta{
						Name:      suiteName,
						Namespace: namespace,
					},
					Spec: complianceoperatorv1alpha1.ComplianceSuiteSpec{
						AutoApplyRemediations: false,
						Scans: []complianceoperatorv1alpha1.ComplianceScanSpecWrapper{
							{
								ComplianceScanSpec: complianceoperatorv1alpha1.ComplianceScanSpec{
									ContentImage: "quay.io/jhrozek/ocp4-openscap-content:remediation_demo",
									Profile:      "xccdf_org.ssgproject.content_profile_coreos-ncp",
									Content:      "ssg-ocp4-ds.xml",
									NodeSelector: selectWorkers,
								},
								Name: workerScanName,
							},
						},
					},
				}

				// Should this be part of some utility function?
				mcClient, err := mcfgClient.NewForConfig(f.KubeConfig)

				err = f.Client.Create(goctx.TODO(), exampleComplianceSuite, &framework.CleanupOptions{TestContext: ctx, Timeout: cleanupTimeout, RetryInterval: cleanupRetryInterval})
				if err != nil {
					return err
				}

				// Ensure that all the scans in the suite have finished and are marked as Done
				err = waitForSuiteScansStatus(t, f, namespace, suiteName, complianceoperatorv1alpha1.PhaseDone)
				if err != nil {
					return err
				}

				// - Get the no-root-logins remediation for workers
				workersNoRootLoginsRemName := fmt.Sprintf("%s-no-direct-root-logins", workerScanName)
				rem := &complianceoperatorv1alpha1.ComplianceRemediation{}
				err = f.Client.Get(goctx.TODO(), types.NamespacedName{Name: workersNoRootLoginsRemName, Namespace: namespace}, rem)
				if err != nil {
					return err
				}
				t.Logf("Remediation found")

				applyRemediation := func() error {
					rem.Spec.Apply = true
					err = f.Client.Update(goctx.TODO(), rem)
					if err != nil {
						t.Errorf("Cannot apply remediation")
						return err
					}
					t.Logf("Remediation applied")
					return nil
				}

				poolHasMc := func(pool *mcfgv1.MachineConfigPool) (bool, error) {
					for _, mc := range pool.Status.Configuration.Source {
						if mc.Name == rem.GetMcName() {
							// Should we wait until the MC is created? I guess not, the poll later would bomb out
							return true, nil
						}
					}

					return false, nil
				}

				err = waitForMachinePoolUpdate(t, mcClient, "worker", applyRemediation, poolHasMc)
				if err != nil {
					t.Errorf("Failed to wait for workers to come back up after applying MC")
					return err
				}
				t.Logf("Machines updated with remediation")

				// We can re-run the scan at this moment and check that we got one less remediation
				secondSuiteName := "test-recheck-remediations"
				secondWorkerScanName := fmt.Sprintf("%s-workers-scan", secondSuiteName)

				secondSuite := &complianceoperatorv1alpha1.ComplianceSuite{
					ObjectMeta: metav1.ObjectMeta{
						Name:      secondSuiteName,
						Namespace: namespace,
					},
					Spec: complianceoperatorv1alpha1.ComplianceSuiteSpec{
						AutoApplyRemediations: false,
						Scans: []complianceoperatorv1alpha1.ComplianceScanSpecWrapper{
							{
								ComplianceScanSpec: complianceoperatorv1alpha1.ComplianceScanSpec{
									ContentImage: "quay.io/jhrozek/ocp4-openscap-content:remediation_demo",
									Profile:      "xccdf_org.ssgproject.content_profile_coreos-ncp",
									Content:      "ssg-ocp4-ds.xml",
									NodeSelector: selectWorkers,
								},
								Name: secondWorkerScanName,
							},
						},
					},
				}

				err = f.Client.Create(goctx.TODO(), secondSuite, &framework.CleanupOptions{TestContext: ctx, Timeout: cleanupTimeout, RetryInterval: cleanupRetryInterval})
				if err != nil {
					return err
				}
				t.Logf("Second scan launched")

				// Ensure that all the scans in the suite have finished and are marked as Done
				err = waitForSuiteScansStatus(t, f, namespace, secondSuiteName, complianceoperatorv1alpha1.PhaseDone)
				if err != nil {
					return err
				}
				t.Logf("Second scan finished")

				// Now the remediation should not be created
				workersNoRootLoginsRemName = fmt.Sprintf("%s-no-direct-root-logins", secondWorkerScanName)
				remCheck := &complianceoperatorv1alpha1.ComplianceRemediation{}
				err = f.Client.Get(goctx.TODO(), types.NamespacedName{Name: workersNoRootLoginsRemName, Namespace: namespace}, remCheck)
				if err == nil {
					return fmt.Errorf("remediation found unexpectedly")
				} else if !errors.IsNotFound(err) {
					t.Errorf("Unexpected error %v", err)
					return err
				}

				// The test should not leave junk around, let's remove the MC and wait for the nodes to stabilize
				// again
				err = mcClient.MachineConfigs().Delete(rem.GetMcName(), &metav1.DeleteOptions{})
				if err != nil {
					return err
				}

				t.Logf("MC deleted, will wait for the machines to come back up")

				dummyAction := func() error {
					return nil
				}
				poolHasNoMc := func(pool *mcfgv1.MachineConfigPool) (bool, error) {
					for _, mc := range pool.Status.Configuration.Source {
						if mc.Name == rem.GetMcName() {
							return false, nil
						}
					}

					return true, nil
				}

				err = waitForMachinePoolUpdate(t, mcClient, "worker", dummyAction, poolHasNoMc)
				if err != nil {
					t.Errorf("Failed to wait for workers to come back up after deleting MC")
					return err
				}

				t.Logf("The test succeeded!")
				return nil
			},
		},
	)
}
