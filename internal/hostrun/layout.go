package hostrun

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"
)

const (
	rootLockName = "root.lock"
	leaseName    = "lease"
	ownerName    = "owner.json"
	tempName     = "t"
	viewsName    = "v"
	procsName    = "p"

	recordSchema = 1
)

func lstatOrCreate(root string) (fs.FileInfo, error) {
	info, err := os.Lstat(root)
	if !errors.Is(err, fs.ErrNotExist) {
		return info, err
	}
	if err := os.Mkdir(root, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return nil, err
	}
	return os.Lstat(root)
}

// The id is short on purpose: macOS caps Unix socket paths at 104 bytes and
// Reviewers create sockets under t/.
var runIDPattern = regexp.MustCompile(`^[0-9a-z]{1,13}-[0-9a-f]{4}$`)

func newRunID() (string, error) {
	var suffix [2]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return "", err
	}
	return strconv.FormatInt(time.Now().Unix(), 36) + "-" + hex.EncodeToString(suffix[:]), nil
}

// Identity names one process instance. Start is the platform's raw start
// stamp and Boot distinguishes boots, so a stamp recorded before a reboot
// never matches a process after it.
type Identity struct {
	PID   int    `json:"pid"`
	Start uint64 `json:"start"`
	Boot  string `json:"boot"`
}

// OwnerRecord is owner.json.
type OwnerRecord struct {
	Schema     int       `json:"schema"`
	ID         string    `json:"id"`
	Owner      Identity  `json:"owner"`
	Created    time.Time `json:"created"`
	Command    string    `json:"command"`
	Version    string    `json:"version"`
	Executable string    `json:"executable"`
}

// TreeMode names how a sentinel bounds its Reviewer tree.
type TreeMode string

const (
	// TreeSubreaper: a process group plus every orphan the sentinel adopts (Linux).
	TreeSubreaper TreeMode = "subreaper"
	// TreeGroup: a process group only (macOS and the BSDs).
	TreeGroup TreeMode = "group"
	// TreeJob: a Job Object with KILL_ON_JOB_CLOSE (Windows).
	TreeJob TreeMode = "job"
	// TreeJobDegraded: the sentinel could not join a Job and bounds only the
	// Reviewer process itself (Windows inside a job that forbids nesting).
	TreeJobDegraded TreeMode = "job-degraded"
)

// ProcessRecord is p/<sentinel-pid>.json: what a reaper needs to kill one
// Reviewer tree whose owner and sentinel are both gone.
type ProcessRecord struct {
	Schema   int      `json:"schema"`
	Sentinel Identity `json:"sentinel"`
	Reviewer Identity `json:"reviewer"`
	// Group is the Reviewer's process group on Unix and zero on Windows.
	Group   int       `json:"group,omitempty"`
	Started time.Time `json:"started"`
	Program string    `json:"program"`
	Mode    TreeMode  `json:"mode"`
}

var errUnknownSchema = errors.New("unknown record schema")

func writeRecord(path string, record any) error {
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

func readRecord(path string, record any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var header struct {
		Schema int `json:"schema"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return err
	}
	if header.Schema != recordSchema {
		return errUnknownSchema
	}
	return json.NewDecoder(bytes.NewReader(data)).Decode(record)
}

func readProcessRecords(dir string) []ProcessRecord {
	entries, err := os.ReadDir(filepath.Join(dir, procsName))
	if err != nil {
		return nil
	}
	var records []ProcessRecord
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		var record ProcessRecord
		if err := readRecord(filepath.Join(dir, procsName, entry.Name()), &record); err != nil {
			continue
		}
		records = append(records, record)
	}
	return records
}
