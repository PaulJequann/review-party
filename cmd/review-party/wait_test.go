package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"reviewparty/internal/engine"
	"reviewparty/internal/model"
	"reviewparty/internal/store"
)

func TestWaitPrintsAFinishedRunExactlyAsRunDid(t *testing.T) {
	fixture := newStatusLedger(t)
	conductor, err := engine.New(engine.Config{UserConfigurationPath: defaultUserConfigurationPath()})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	var bundles []model.ReviewBundle
	var record model.ReviewRecord
	fixture.write(func(ledger *store.LedgerRecordStore) error {
		for _, id := range []model.ReviewBundleID{completedBundleID, incompleteBundleID} {
			bundle, err := ledger.LoadReviewBundle(id)
			if err != nil {
				return err
			}
			bundles = append(bundles, bundle)
		}
		var err error
		record, err = ledger.Load(incompleteReviewID)
		return err
	})
	bundleOutput := func(bundle model.ReviewBundle, full bool) (reviewReport, error) {
		return bundleReport(ctx, conductor, bundle, full), nil
	}
	for _, test := range []struct {
		id       string
		wantExit int
		report   func(full bool) (reviewReport, error)
	}{
		{completedBundleID, 0, func(full bool) (reviewReport, error) { return bundleOutput(bundles[0], full) }},
		{incompleteBundleID, usageExitCode, func(full bool) (reviewReport, error) { return bundleOutput(bundles[1], full) }},
		{incompleteReviewID, usageExitCode, func(full bool) (reviewReport, error) { return recordReport(record, full), nil }},
	} {
		for _, flags := range [][]string{{}, {"--format", "json"}, {"--format", "json", "--full"}} {
			format, full := "human", false
			if len(flags) > 0 {
				format, full = "json", len(flags) == 3
			}
			report, err := test.report(full)
			if err != nil {
				t.Fatal(err)
			}
			var want bytes.Buffer
			if err := printReport(&want, report, reportOptions{format: format, configuration: defaultUserConfigurationPath()}); err != nil {
				t.Fatal(err)
			}
			exit, stdout, stderr := runCLI(append([]string{"wait", test.id}, flags...)...)
			if exit != test.wantExit || stdout != want.String() || stderr != "" {
				t.Fatalf("wait %s %v exit = %d (want %d), stderr = %q, stdout =\n%s\nwant =\n%s", test.id, flags, exit, test.wantExit, stderr, stdout, want.String())
			}
		}
	}
}

func TestWaitReportsAStoppedBundleAsRunsHardFailure(t *testing.T) {
	newStatusLedger(t)
	exit, stdout, stderr := runCLI("wait", stoppedBundleID)
	if exit != 1 || stdout != "" || stderr != "review-party: context canceled\n" {
		t.Fatalf("exit = %d, stdout = %q, stderr = %q; want run's hard-stop failure", exit, stdout, stderr)
	}
}

func TestWaitTimesOutOnARunThatIsStillInFlight(t *testing.T) {
	newStatusLedger(t)
	exit, stdout, stderr := runCLI("wait", runningBundleID, "--timeout", "30ms")
	if exit != 1 || stdout != "" || stderr != "review-party: timed out after 30ms waiting for "+runningBundleID+" (running)\n" {
		t.Fatalf("exit = %d, stdout = %q, stderr = %q", exit, stdout, stderr)
	}
}

// flippingConductor finishes the pending Review in the ledger right after
// wait's first check observes it in flight.
type flippingConductor struct {
	*engine.Conductor
	fixture statusLedger
	checks  int
}

func (conductor *flippingConductor) Status(ctx context.Context, id string) (model.ReviewStatus, error) {
	status, err := conductor.Conductor.Status(ctx, id)
	conductor.checks++
	if conductor.checks == 1 {
		conductor.fixture.write(func(ledger *store.LedgerRecordStore) error {
			return ledger.Save(conductor.fixture.record(pendingReviewID, model.LifecycleCompleted, 5))
		})
	}
	return status, err
}

func TestWaitReturnsOnceAnInFlightReviewFinishes(t *testing.T) {
	fixture := newStatusLedger(t)
	engineConductor, err := engine.New(engine.Config{UserConfigurationPath: defaultUserConfigurationPath()})
	if err != nil {
		t.Fatal(err)
	}
	conductor := &flippingConductor{Conductor: engineConductor, fixture: fixture}
	var stdout, stderr bytes.Buffer
	exit := executeWait(context.Background(), conductor, waitOptions{
		id: pendingReviewID, format: "human", configuration: defaultUserConfigurationPath(), interval: time.Millisecond,
	}, commandIO{output: &stdout, errors: &stderr})
	if exit != 0 || conductor.checks != 2 || !strings.HasPrefix(stdout.String(), "review "+pendingReviewID+" · completed · findings · 2 finding(s)\n") {
		t.Fatalf("exit = %d after %d checks, stdout = %q, stderr = %q", exit, conductor.checks, stdout.String(), stderr.String())
	}
}

func TestWaitPrintsABundleWithAnUnreadableMemberAndFailsAsRunDoes(t *testing.T) {
	fixture := newStatusLedger(t)
	conductor, err := engine.New(engine.Config{UserConfigurationPath: defaultUserConfigurationPath()})
	if err != nil {
		t.Fatal(err)
	}
	var bundle model.ReviewBundle
	fixture.write(func(ledger *store.LedgerRecordStore) error {
		bundle, err = ledger.LoadReviewBundle(unreadableBundleID)
		return err
	})
	report := bundleReport(context.Background(), conductor, bundle, false)
	var want bytes.Buffer
	if err := printReport(&want, report, reportOptions{format: "json"}); err != nil {
		t.Fatal(err)
	}
	exit, stdout, stderr := runCLI("wait", unreadableBundleID, "--format", "json")
	if exit != 1 || stdout != want.String() || stderr != "review-party: "+report.readFailure().Error()+"\n" {
		t.Fatalf("exit = %d, stderr = %q, stdout =\n%s\nwant =\n%s", exit, stderr, stdout, want.String())
	}
}
