package configuration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reviewparty/internal/configuration"
)

func TestPublishRefusesZeroValuePlan(t *testing.T) {
	manager := integrityManager(t, t.TempDir())

	err := manager.Publish(configuration.Plan{})

	if err == nil || !strings.Contains(err.Error(), "invalid change plan") {
		t.Fatalf("Publish() error = %v, want invalid change plan", err)
	}
}

func TestPublishRefusesWhenTargetAppearsOrChanges(t *testing.T) {
	for _, test := range []struct {
		name    string
		initial string
		changed string
	}{
		{name: "existing file changes", initial: `{"schema_version":1,"state_directory":"/before"}`, changed: `{"schema_version":1,"state_directory":"/changed"}`},
		{name: "absent file appears", changed: `{"schema_version":1,"state_directory":"/appeared"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "config.json")
			if test.initial != "" {
				writeIntegrityDocument(t, path, test.initial)
			}
			manager := integrityManager(t, root)
			plan := integrityPlan(t, manager, "", configuration.SetStateDirectory{Directory: "/planned"})
			writeIntegrityDocument(t, path, test.changed)

			err := manager.Publish(plan)

			assertStalePlanError(t, err, path)
			assertIntegrityContents(t, path, test.changed)
		})
	}
}

func TestPublishRefusesWhenExistingFileDisappears(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.json")
	writeIntegrityDocument(t, path, `{"schema_version":1,"state_directory":"/before"}`)
	manager := integrityManager(t, root)
	plan := integrityPlan(t, manager, "", configuration.SetStateDirectory{Directory: "/planned"})
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	err := manager.Publish(plan)

	assertStalePlanError(t, err, path)
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("target was recreated: %v", statErr)
	}
}

func TestPublishRefusesWhenTargetBecomesSymlinkOrSpecialFile(t *testing.T) {
	for _, test := range []struct {
		name    string
		replace func(*testing.T, string)
	}{
		{name: "symlink", replace: replaceIntegrityTargetWithSymlink},
		{name: "directory", replace: replaceIntegrityTargetWithDirectory},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "config.json")
			writeIntegrityDocument(t, path, `{"schema_version":1,"state_directory":"/before"}`)
			manager := integrityManager(t, root)
			plan := integrityPlan(t, manager, "", configuration.SetStateDirectory{Directory: "/planned"})
			test.replace(t, path)

			err := manager.Publish(plan)

			assertStalePlanError(t, err, path)
		})
	}
}

func TestPublishAcceptsUnchangedPlan(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.json")
	writeIntegrityDocument(t, path, `{"schema_version":1,"state_directory":"/before"}`)
	manager := integrityManager(t, root)
	plan := integrityPlan(t, manager, "", configuration.SetStateDirectory{Directory: "/planned"})

	if err := manager.Publish(plan); err != nil {
		t.Fatal(err)
	}

	assertIntegrityContents(t, path, "\"state_directory\": \"/planned\"")
}

func TestPublishDoesNotWriteAnyTargetWhenOneIsStale(t *testing.T) {
	globalRoot := t.TempDir()
	repository := t.TempDir()
	globalPath := filepath.Join(globalRoot, "config.json")
	repositoryPath := filepath.Join(repository, ".reviewparty", "config.json")
	globalBefore := `{"schema_version":1,"state_directory":"/before"}`
	repositoryBefore := `{"schema_version":1,"reviews":{"concurrency_limit":1,"global":[],"repository":[]}}`
	writeIntegrityDocument(t, globalPath, globalBefore)
	writeIntegrityDocument(t, repositoryPath, repositoryBefore)
	manager := integrityManager(t, globalRoot)
	plan := integrityPlan(t, manager, repository,
		configuration.SetStateDirectory{Directory: "/planned"},
		configuration.SetReviewSelection{Selection: configuration.ReviewSelection{ConcurrencyLimit: 2}},
	)
	repositoryChanged := `{"schema_version":1,"reviews":{"concurrency_limit":3,"global":[],"repository":[]}}`
	writeIntegrityDocument(t, repositoryPath, repositoryChanged)

	err := manager.Publish(plan)

	assertStalePlanError(t, err, repositoryPath)
	assertIntegrityContents(t, globalPath, globalBefore)
	assertIntegrityContents(t, repositoryPath, repositoryChanged)
}

func integrityManager(t *testing.T, globalRoot string) *configuration.Manager {
	t.Helper()
	return configuration.NewManager(configuration.Options{GlobalRoot: globalRoot})
}

func integrityPlan(t *testing.T, manager *configuration.Manager, repository string, intents ...configuration.Intent) configuration.Plan {
	t.Helper()
	plan, err := manager.Plan(configuration.Repository(repository), intents)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Valid() {
		t.Fatalf("Plan() invalid: %s", plan.Reason())
	}
	return plan
}

func writeIntegrityDocument(t *testing.T, path, payload string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
}

func replaceIntegrityTargetWithSymlink(t *testing.T, path string) {
	t.Helper()
	target := filepath.Join(filepath.Dir(path), "other.json")
	writeIntegrityDocument(t, target, `{"schema_version":1}`)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Base(target), path); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
}

func replaceIntegrityTargetWithDirectory(t *testing.T, path string) {
	t.Helper()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func assertStalePlanError(t *testing.T, err error, path string) {
	t.Helper()
	if err == nil {
		t.Fatal("Publish() succeeded, want stale plan error")
	}
	if !strings.Contains(err.Error(), "stale change plan") {
		t.Fatalf("Publish() error = %v, want stale plan error", err)
	}
	if !strings.Contains(err.Error(), path) {
		t.Fatalf("Publish() error = %v, want target path %q", err, path)
	}
}

func assertIntegrityContents(t *testing.T, path, want string) {
	t.Helper()
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), want) {
		t.Fatalf("%s contents = %q, want %q", path, payload, want)
	}
}
