package reviewparty

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

type ProfileInitialization struct {
	Repository      string
	Global          bool
	GlobalDirectory string
}

type ProfileInitializationResult struct {
	Directory string
	Created   []string
	Existing  []string
}

type starterProfileFile struct {
	path    string
	payload []byte
}

func InitializeProfiles(initialization ProfileInitialization) (ProfileInitializationResult, error) {
	directory, permissions, err := initializationDirectory(initialization)
	if err != nil {
		return ProfileInitializationResult{}, err
	}
	profilesDirectory := filepath.Join(directory, "profiles")
	if err := os.MkdirAll(profilesDirectory, permissions); err != nil {
		return ProfileInitializationResult{}, fmt.Errorf("create profile directory %q: %w", profilesDirectory, err)
	}

	files, err := starterProfileFiles(directory, profilesDirectory)
	if err != nil {
		return ProfileInitializationResult{}, err
	}
	filePermissions := profileInitializationFilePermissions(initialization.Global)
	result := ProfileInitializationResult{Directory: directory}
	for _, file := range files {
		created, writeErr := writeNewProfileFile(file.path, file.payload, filePermissions)
		if writeErr != nil {
			return ProfileInitializationResult{}, writeErr
		}
		appendProfileInitializationResult(&result, file.path, created)
	}
	return result, nil
}

func starterProfileFiles(directory, profilesDirectory string) ([]starterProfileFile, error) {
	configPayload, err := json.MarshalIndent(profileConfig{
		Schema:          profileConfigSchema,
		DefaultProfile:  "bugs",
		DefaultReviewer: defaultReviewer,
	}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode starter profile config: %w", err)
	}
	configPayload = append(configPayload, '\n')
	bugsPayload, err := packagedProfileFiles.ReadFile("profiles/bugs.md")
	if err != nil {
		return nil, fmt.Errorf("read packaged bugs profile: %w", err)
	}
	return []starterProfileFile{
		{path: filepath.Join(directory, "config.json"), payload: configPayload},
		{path: filepath.Join(profilesDirectory, "bugs.md"), payload: bugsPayload},
	}, nil
}

func profileInitializationFilePermissions(global bool) fs.FileMode {
	if global {
		return 0o600
	}
	return 0o644
}

func appendProfileInitializationResult(result *ProfileInitializationResult, path string, created bool) {
	if created {
		result.Created = append(result.Created, path)
		return
	}
	result.Existing = append(result.Existing, path)
}

func initializationDirectory(initialization ProfileInitialization) (string, fs.FileMode, error) {
	if initialization.Global {
		directory := initialization.GlobalDirectory
		if directory == "" {
			directory = defaultGlobalProfileDirectory()
		}
		if directory == "" {
			return "", 0, errors.New("resolve global profile directory: user home is unavailable; set REVIEW_PARTY_HOME")
		}
		return directory, 0o700, nil
	}
	root, err := resolveRepositoryRoot(initialization.Repository)
	if err != nil {
		return "", 0, err
	}
	return filepath.Join(root, ".reviewparty"), 0o755, nil
}

func writeNewProfileFile(path string, payload []byte, permissions fs.FileMode) (bool, error) {
	return writeNewProfileFileWith(path, payload, permissions, writeProfilePayload)
}

func writeNewProfileFileWith(path string, payload []byte, permissions fs.FileMode, write func(*os.File, []byte) error) (bool, error) {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, permissions)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return existingProfileFile(path)
		}
		return false, fmt.Errorf("create profile file %q: %w", path, err)
	}
	complete := false
	defer func() {
		if !complete {
			os.Remove(path)
		}
	}()
	if err := write(file, payload); err != nil {
		file.Close()
		return false, fmt.Errorf("write profile file %q: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return false, fmt.Errorf("close profile file %q: %w", path, err)
	}
	complete = true
	return true, nil
}

func existingProfileFile(path string) (bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return false, fmt.Errorf("inspect existing profile file %q: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return false, fmt.Errorf("existing profile path %q must be a regular file, not a symlink or special file", path)
	}
	return false, nil
}

func writeProfilePayload(file *os.File, payload []byte) error {
	if _, err := file.Write(payload); err != nil {
		return err
	}
	return file.Sync()
}
