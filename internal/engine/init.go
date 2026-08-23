package engine

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"reviewparty/internal/configuration"
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

type ProfileCreation struct {
	Name            string
	Repository      string
	Global          bool
	GlobalDirectory string
	Blank           bool
	PackagedProfile string
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
	profileRoot, err := prepareProfileDirectory(directory, permissions)
	if err != nil {
		return ProfileInitializationResult{}, err
	}
	defer profileRoot.Close()

	files, err := starterProfileFiles(directory, profilesDirectory)
	if err != nil {
		return ProfileInitializationResult{}, err
	}
	filePermissions := profileInitializationFilePermissions(initialization.Global)
	result := ProfileInitializationResult{Directory: directory}
	for _, file := range files {
		created, writeErr := writeProfileFile(profileRoot, file, filePermissions)
		if writeErr != nil {
			return ProfileInitializationResult{}, writeErr
		}
		appendProfileInitializationResult(&result, file.path, created)
	}
	return result, nil
}

func CreateProfile(creation ProfileCreation) (ProfileInitializationResult, error) {
	if err := validateAuthoredName(creation.Name); err != nil {
		return ProfileInitializationResult{}, fmt.Errorf("invalid Profile name %q: %w", creation.Name, err)
	}
	if creation.Blank == (creation.PackagedProfile != "") {
		return ProfileInitializationResult{}, errors.New("choose exactly one starting point: blank or packaged Profile")
	}
	directory, permissions, err := initializationDirectory(ProfileInitialization{
		Repository: creation.Repository, Global: creation.Global, GlobalDirectory: creation.GlobalDirectory,
	})
	if err != nil {
		return ProfileInitializationResult{}, err
	}
	profileRoot, err := prepareProfileDirectory(directory, permissions)
	if err != nil {
		return ProfileInitializationResult{}, err
	}
	defer profileRoot.Close()
	payload, err := profileCreationPayload(creation)
	if err != nil {
		return ProfileInitializationResult{}, err
	}
	path := filepath.Join(directory, "profiles", creation.Name+".md")
	created, err := writeProfileFile(profileRoot, starterProfileFile{path: path, payload: payload}, profileInitializationFilePermissions(creation.Global))
	if err != nil {
		return ProfileInitializationResult{}, err
	}
	result := ProfileInitializationResult{Directory: directory}
	appendProfileInitializationResult(&result, path, created)
	return result, nil
}

func profileCreationPayload(creation ProfileCreation) ([]byte, error) {
	if creation.Blank {
		return []byte("Describe the Reviewer Judgment for this Profile.\n"), nil
	}
	if err := validateAuthoredName(creation.PackagedProfile); err != nil {
		return nil, fmt.Errorf("invalid packaged Profile name %q: %w", creation.PackagedProfile, err)
	}
	payload, err := packagedProfileFiles.ReadFile("profiles/" + creation.PackagedProfile + ".md")
	if err != nil {
		return nil, fmt.Errorf("packaged Profile %q is unavailable", creation.PackagedProfile)
	}
	return payload, nil
}

func prepareProfileDirectory(directory string, permissions fs.FileMode) (*os.Root, error) {
	anchor, err := nearestExistingDirectory(filepath.Dir(directory))
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(anchor)
	if err != nil {
		return nil, fmt.Errorf("open profile directory anchor %q: %w", anchor, err)
	}
	relativeDirectory, err := filepath.Rel(anchor, directory)
	if err != nil {
		root.Close()
		return nil, fmt.Errorf("resolve profile directory %q within %q: %w", directory, anchor, err)
	}
	profilesPath := filepath.Join(relativeDirectory, "profiles")
	if err := validateProfileDirectories(root, anchor, profilesPath); err != nil {
		root.Close()
		return nil, err
	}
	if err := root.MkdirAll(profilesPath, permissions); err != nil {
		root.Close()
		return nil, fmt.Errorf("create profile directory %q: %w", filepath.Join(anchor, profilesPath), err)
	}
	if err := validateProfileDirectories(root, anchor, profilesPath); err != nil {
		root.Close()
		return nil, err
	}
	return root, nil
}

func nearestExistingDirectory(directory string) (string, error) {
	for candidate := filepath.Clean(directory); ; candidate = filepath.Dir(candidate) {
		info, err := os.Lstat(candidate)
		if err == nil {
			if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
				return "", fmt.Errorf("profile directory ancestor %q must be a directory, not a symlink or special file", candidate)
			}
			return candidate, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("inspect profile directory ancestor %q: %w", candidate, err)
		}
		if filepath.Dir(candidate) == candidate {
			return "", fmt.Errorf("resolve existing profile directory ancestor for %q", directory)
		}
	}
}

func validateProfileDirectories(root *os.Root, anchor, profilesPath string) error {
	current := ""
	for _, component := range strings.Split(filepath.Clean(profilesPath), string(filepath.Separator)) {
		current = filepath.Join(current, component)
		path := current
		info, inspectErr := root.Lstat(path)
		if inspectErr != nil {
			if errors.Is(inspectErr, fs.ErrNotExist) {
				continue
			}
			return fmt.Errorf("inspect profile directory %q: %w", filepath.Join(anchor, path), inspectErr)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("profile directory %q must be a directory, not a symlink or special file", filepath.Join(anchor, path))
		}
	}
	return nil
}

func writeProfileFile(profileRoot *os.Root, file starterProfileFile, permissions fs.FileMode) (bool, error) {
	relative, err := filepath.Rel(profileRoot.Name(), file.path)
	if err != nil {
		return false, fmt.Errorf("resolve profile file %q within repository: %w", file.path, err)
	}
	operations := profileFileOperations{
		open: func() (*os.File, error) {
			return profileRoot.OpenFile(relative, os.O_WRONLY|os.O_CREATE|os.O_EXCL, permissions)
		},
		inspect: func() (fs.FileInfo, error) { return profileRoot.Lstat(relative) },
		remove:  func() error { return profileRoot.Remove(relative) },
	}
	return writeNewProfileFileUsing(file.path, file.payload, operations, writeProfilePayload)
}

func starterProfileFiles(_ string, profilesDirectory string) ([]starterProfileFile, error) {
	names := SupportedProfiles()
	files := make([]starterProfileFile, 0, len(names))
	for _, name := range names {
		payload, err := packagedProfileFiles.ReadFile("profiles/" + name + ".md")
		if err != nil {
			return nil, fmt.Errorf("read packaged %s Profile: %w", name, err)
		}
		files = append(files, starterProfileFile{path: filepath.Join(profilesDirectory, name+".md"), payload: payload})
	}
	return files, nil
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
			directory = configuration.DefaultPersonalRoot()
		}
		if directory == "" {
			return "", 0, errors.New("resolve personal configuration root: set XDG_CONFIG_HOME or HOME")
		}
		absolute, err := filepath.Abs(directory)
		if err != nil {
			return "", 0, fmt.Errorf("resolve personal profile directory %q: %w", directory, err)
		}
		return absolute, 0o700, nil
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
	operations := profileFileOperations{
		open:    func() (*os.File, error) { return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, permissions) },
		inspect: func() (fs.FileInfo, error) { return os.Lstat(path) },
		remove:  func() error { return os.Remove(path) },
	}
	return writeNewProfileFileUsing(path, payload, operations, write)
}

type profileFileOperations struct {
	open    func() (*os.File, error)
	inspect func() (fs.FileInfo, error)
	remove  func() error
}

func writeNewProfileFileUsing(path string, payload []byte, operations profileFileOperations, write func(*os.File, []byte) error) (bool, error) {
	file, err := operations.open()
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return existingProfileFile(path, operations.inspect)
		}
		return false, fmt.Errorf("create profile file %q: %w", path, err)
	}
	complete := false
	defer func() {
		if !complete {
			operations.remove()
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

func existingProfileFile(path string, inspect func() (fs.FileInfo, error)) (bool, error) {
	info, err := inspect()
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
