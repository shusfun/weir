package center

import (
	"strings"
	"testing"
)

func TestADRTraceSmoke(t *testing.T) {
	logs := captureLog(t)
	t.Cleanup(func() {
		if t.Failed() {
			for _, line := range strings.Split(logs.String(), "\n") {
				if strings.Contains(line, "operation=certificate.") {
					t.Log(line)
				}
			}
		}
	})
	TestBrowserSessionsBootCancellationAndOwnership(t)
	TestUoTStableIdentityAndReceiverRoutes(t)
	TestCatalogRefreshKeepsIdentityAndUnavailableReferences(t)
	TestCatalogGrantsAndLinkSelectionsIntersect(t)
	TestRenewalOnlyMovesMatchingFiniteLinks(t)
	TestBackupPreviewBindingAndStaleness(t)
	TestDailyUsageRetriesEpochAndInternalHop(t)
	TestGateTrafficConfigureRevisionAndObservation(t)
	TestGateCapacityNoticesAndAutomaticPlan(t)
	TestExitCredentialsRequireExplicitAuditedAdminReveal(t)
	TestGateDeletionPreviewAndAtomicCleanup(t)
	TestXHTTPDeployedNodeUsesNativeClient(t)
	TestIPCertViaACMEThenExpiryAndRenewal(t)
	TestOutboundExecutionPlanAndStableSealedPorts(t)
	TestSourceStableSelectionAndLegacyBinding(t)
	TestExitLinkConfirmationBoundToConnection(t)
	TestRuleMeterIsolatedLabelsReplayAndBilling(t)
	TestDomainObservationReplayEpochPrivacyAndAuthorization(t)
	TestDomainObserverStableSealedPorts(t)
	TestDomainDirectRuleCASAndAtomicCallback(t)
	for _, operation := range []string{"browser.test.create", "browser.test.finish", "traffic.domains.report", "traffic.domains.aggregate", "traffic.domains.plan", "traffic.domains.ignore", "traffic.domains.direct", "traffic.rules.report", "traffic.rules.aggregate", "proxy.link.inspect", "proxy.source.identity", "proxy.execution.plan", "certificate.result.persist", "node.probe.execute", "node.catalog.refresh", "node.authorize", "subscription.link.save", "user.plan.update", "backup.preview", "backup.restore", "traffic.report", "traffic.aggregate", "traffic.host.sample", "traffic.host.configure", "traffic.host.notices", "subscription.capacity.plan", "proxy.credentials.reveal", "gate.delete.preview", "gate.delete.references", "gate.delete"} {
		found := false
		for _, line := range strings.Split(logs.String(), "\n") {
			if strings.Contains(line, "operation="+operation+" ") && strings.Contains(line, "outcome=succeeded") && strings.Contains(line, "trace=") && strings.Contains(line, "code.file.path=") && strings.Contains(line, "vcs.ref.head.revision=") {
				found = true
				t.Log(line)
				break
			}
		}
		if !found {
			t.Fatalf("missing correlated success stage %s", operation)
		}
	}
}
