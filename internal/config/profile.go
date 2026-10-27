// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// File layout:
//
//	Dir()/config.yaml          profiles: project_id, region, defaults  (0600)
//	Dir()/credentials.yaml     profiles: key_id, secret, s3            (0600)
//	CacheDir()/token-<p>.json  IAM token cache                          (0600)
const (
	appDir          = AppName
	settingsName    = "config.yaml"
	credentialsName = "credentials.yaml"

	// EnvConfigDir overrides Dir(); the token cache then lives in its
	// cache/ subdirectory, so one variable isolates everything (tests, CI).
	EnvConfigDir = "CLOUDRU_CONFIG_DIR"
)

// Profile holds the non-secret settings of a profile (config.yaml).
type Profile struct {
	ProjectID string `yaml:"project_id,omitempty"`
	Region    string `yaml:"region,omitempty"`
	Flavor    string `yaml:"flavor,omitempty"`
	ImageID   string `yaml:"image_id,omitempty"`
	DiskSize  int    `yaml:"disk_size,omitempty"`
	Timeout   int    `yaml:"timeout,omitempty"`
}

// Credentials holds the secrets of a profile (credentials.yaml).
type Credentials struct {
	KeyID  string    `yaml:"key_id,omitempty"`
	Secret string    `yaml:"secret,omitempty"`
	S3     *S3Config `yaml:"s3,omitempty"`
}

type settingsFile struct {
	Profiles map[string]Profile `yaml:"profiles"`
}

type credentialsFile struct {
	Profiles map[string]Credentials `yaml:"profiles"`
}

// Dir is the directory with config.yaml and credentials.yaml.
func Dir() string {
	if d := os.Getenv(EnvConfigDir); d != "" {
		return d
	}
	base := configBase()
	if base == "" {
		return ""
	}
	return filepath.Join(base, appDir)
}

// userConfigDir is os.UserConfigDir; tests point it at a temp dir.
var userConfigDir = os.UserConfigDir

// configBase is the user config directory, or "" when neither it nor the
// home directory is known (no HOME under systemd, launchd, a container).
// It never falls back to a path relative to the working directory: the
// profiles hold the key, and the working directory may be a repository.
func configBase() string {
	base, err := userConfigDir()
	if err != nil {
		home, herr := os.UserHomeDir()
		if herr != nil || home == "" {
			return ""
		}
		base = filepath.Join(home, ".config")
	}
	return base
}

// NoConfigDir is the error of a command that has to read or write the
// profiles when Dir is "".
func NoConfigDir() error {
	return clerrors.New(clerrors.CodeConfigInvalid,
		i18n.Tf("no directory for the profiles: neither %s nor HOME (on Windows APPDATA) is set",
			"нет каталога для профилей: не заданы ни %s, ни HOME (в Windows — APPDATA)", EnvConfigDir),
		i18n.Tf("Set %s to a private directory, or HOME; the key can also come from the environment (CLOUDRU_KEY_ID, CLOUDRU_SECRET)",
			"Задайте %s (личный каталог) или HOME; ключ можно передать и переменными окружения (CLOUDRU_KEY_ID, CLOUDRU_SECRET)", EnvConfigDir))
}

// NotADirectory is the error of a configuration directory that is a file:
// CLOUDRU_CONFIG_DIR set to config.yaml itself, say. Reading the profiles
// said "open …/config.yaml: not a directory" and asked to check the file
// permissions.
func NotADirectory(dir string) error {
	hint := i18n.Tf("Remove the file %s or set %s to a directory", "Удалите файл %s или задайте в %s каталог", dir, EnvConfigDir)
	if os.Getenv(EnvConfigDir) != "" {
		hint = i18n.Tf("%s names the directory of the profiles, not a file: set it to a directory (config.yaml and credentials.yaml go inside)",
			"%s задаёт каталог профилей, а не файл: укажите в нём каталог (config.yaml и credentials.yaml лежат внутри)", EnvConfigDir)
	}
	return clerrors.New(clerrors.CodeConfigInvalid,
		i18n.Tf("%s is a file, not a directory", "%s — файл, а не каталог", dir), hint)
}

// MigrateLegacyDir copies the profiles of the old binary, once: when
// the legacy <user config dir>/cloudru-vm exists and <user config dir>/evo does not,
// config.yaml and credentials.yaml are copied into a new private directory
// (0700, files 0600; on Windows an owner-only ACL). The legacy directory is
// left as it was, for an older binary still in use. The token cache is not
// copied: it lives elsewhere and the next call gets a fresh token. With
// CLOUDRU_CONFIG_DIR set nothing is done: the variable names the directory.
//
// It returns a message for the user when it copied something.
func MigrateLegacyDir() (string, error) {
	if os.Getenv(EnvConfigDir) != "" {
		return "", nil
	}
	base := configBase()
	if base == "" {
		return "", nil
	}
	dir := filepath.Join(base, appDir)
	legacy := filepath.Join(base, LegacyAppName)
	if exists(dir) || !exists(legacy) {
		return "", nil
	}
	// Into a temporary directory first and then one rename: an interrupted
	// copy never leaves a half-filled evo/ that would stop the next try.
	tmp := filepath.Join(base, "."+appDir+".migrate-"+strconv.Itoa(os.Getpid()))
	_ = os.RemoveAll(tmp)
	if err := MkdirPrivate(tmp); err != nil {
		return "", fmt.Errorf(i18n.T("create %s: %w", "создание %s: %w"), tmp, err)
	}
	defer os.RemoveAll(tmp) // no-op after the rename
	copied := 0
	for _, name := range []string{settingsName, credentialsName} {
		src := filepath.Join(legacy, name)
		st, err := os.Lstat(src)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf(i18n.T("read %s: %w", "чтение %s: %w"), src, err)
		}
		if !st.Mode().IsRegular() {
			return "", fmt.Errorf("%s is not a regular file", src)
		}
		data, err := os.ReadFile(src)
		if err != nil {
			return "", fmt.Errorf(i18n.T("read %s: %w", "чтение %s: %w"), src, err)
		}
		if err := WriteFilePrivate(filepath.Join(tmp, name), data); err != nil {
			return "", err
		}
		copied++
	}
	if err := os.Rename(tmp, dir); err != nil {
		if exists(dir) { // another process got there first
			return "", nil
		}
		return "", fmt.Errorf(i18n.T("create %s: %w", "создание %s: %w"), dir, err)
	}
	if copied == 0 {
		return "", nil
	}
	return i18n.Tf("profiles copied from %s to %s (the legacy directory is left as is; remove it when no older %s is used)",
		"профили скопированы из %s в %s (старый каталог не тронут; удалите его, когда старый %s больше не нужен)",
		legacy, dir, LegacyAppName), nil
}

// CacheDir is the directory with the token cache.
func CacheDir() string {
	if d := os.Getenv(EnvConfigDir); d != "" {
		return filepath.Join(d, "cache")
	}
	base, err := os.UserCacheDir()
	if err != nil {
		home, herr := os.UserHomeDir()
		if herr != nil || home == "" {
			return "" // as configBase: never relative to the working directory
		}
		base = filepath.Join(home, ".cache")
	}
	return filepath.Join(base, appDir)
}

// SettingsPath is the path of config.yaml.
func SettingsPath() string { return inDir(Dir(), settingsName) }

// CredentialsPath is the path of credentials.yaml.
func CredentialsPath() string { return inDir(Dir(), credentialsName) }

// TokenCachePath is the IAM token cache file of a profile.
// It is "" when no cache directory is known: the token is then not cached.
func TokenCachePath(profile string) string {
	return inDir(CacheDir(), "token-"+profile+".json")
}

// inDir joins dir and name, or is "" when dir is unknown.
func inDir(dir, name string) string {
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, name)
}

var profileNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)

// ValidateProfileName rejects names that cannot be used as part of a file
// name (the token cache is named after the profile).
func ValidateProfileName(name string) error {
	if !profileNameRe.MatchString(name) {
		return clerrors.New(clerrors.CodeConfigInvalid,
			i18n.Tf("invalid profile name %q", "недопустимое имя профиля %q", name),
			i18n.T("Use letters, digits, '-', '_' and '.', up to 64 characters",
				"Допустимы латинские буквы, цифры, '-', '_' и '.', не длиннее 64 символов"))
	}
	return nil
}

// PermissionsEnforced reports whether Unix file modes are meaningful here.
// On Windows os.Chmod only toggles the read-only bit.
func PermissionsEnforced() bool { return runtime.GOOS != "windows" }

// LooseMode reports whether a file mode gives group or others any access.
func LooseMode(m fs.FileMode) bool { return m.Perm()&0o077 != 0 }

func readYAML(path string, v any) (exists bool, err error) {
	if path == "" { // no config directory: no profiles
		return false, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) || errors.Is(err, syscall.ENOTDIR) {
			// A file in the way of the path: ENOTDIR on Linux and macOS,
			// "path not found" on Windows. The nearest ancestor that
			// exists tells which.
			dir := filepath.Dir(path)
			fi, serr := os.Stat(dir)
			for serr != nil && filepath.Dir(dir) != dir {
				dir = filepath.Dir(dir)
				fi, serr = os.Stat(dir)
			}
			if serr == nil && !fi.IsDir() {
				return false, NotADirectory(dir)
			}
			if os.IsNotExist(err) {
				return false, nil
			}
		}
		// The text of the system without "open <path>:", which repeated
		// the path; a missing permission in the language of the CLI.
		cause := err.Error()
		var pe *fs.PathError
		if errors.As(err, &pe) {
			cause = pe.Err.Error()
		}
		if errors.Is(err, fs.ErrPermission) {
			cause = i18n.T("permission denied", "нет прав на чтение")
		}
		return false, clerrors.Wrap(clerrors.CodeConfigInvalid, i18n.T("reading ", "чтение ")+path+": "+cause,
			i18n.T("Check the file permissions", "Проверьте права на файл"), err)
	}
	if err := yaml.Unmarshal(data, v); err != nil {
		return true, clerrors.Wrap(clerrors.CodeConfigInvalid, i18n.Tf("parsing %s: %v", "разбор %s: %v", path, err),
			i18n.Tf("Fix the YAML syntax or recreate the profile with '%s configure'",
				"Исправьте синтаксис YAML или создайте профиль заново: '%s configure'", AppName), err)
	}
	return true, nil
}

func readSettingsFile() (settingsFile, error) {
	var f settingsFile
	_, err := readYAML(SettingsPath(), &f)
	return f, err
}

// readCredentialsFile also warns when the file is readable by others.
func readCredentialsFile() (credentialsFile, []string, error) {
	var f credentialsFile
	path := CredentialsPath()
	exists, err := readYAML(path, &f)
	if err != nil || !exists {
		return f, nil, err
	}
	var warnings []string
	if PermissionsEnforced() {
		if st, err := os.Stat(path); err == nil && LooseMode(st.Mode()) {
			warnings = append(warnings, i18n.Tf(
				"%s is accessible by other users (mode %04o); run: chmod 600 %s",
				"%s доступен другим пользователям (права %04o); выполните: chmod 600 %s", path, st.Mode().Perm(), path))
		}
		// A 0600 file is no protection in a directory others can write:
		// they replace credentials.yaml (with their key, or a symlink) and
		// the token cache.
		dir := filepath.Dir(path)
		if st, err := os.Stat(dir); err == nil && st.Mode().Perm()&0o022 != 0 {
			warnings = append(warnings, i18n.Tf(
				"%s is writable by other users (mode %04o): they can replace credentials.yaml; run: chmod 700 %s",
				"в %s могут писать другие пользователи (права %04o) и подменить credentials.yaml; выполните: chmod 700 %s",
				dir, st.Mode().Perm(), dir))
		}
	} else {
		// Windows: the same two checks on the ACL. Files written by older
		// versions inherited the ACL of the directory.
		for _, w := range []string{PrivateFileWarning(path), PrivateDirWarning(filepath.Dir(path))} {
			if w != "" {
				warnings = append(warnings, w)
			}
		}
	}
	return f, warnings, nil
}

// LoadProfile returns the stored settings and credentials of a profile.
func LoadProfile(name string) (Profile, Credentials, bool, error) {
	s, err := readSettingsFile()
	if err != nil {
		return Profile{}, Credentials{}, false, err
	}
	c, _, err := readCredentialsFile()
	if err != nil {
		return Profile{}, Credentials{}, false, err
	}
	p, ok1 := s.Profiles[name]
	cr, ok2 := c.Profiles[name]
	return p, cr, ok1 || ok2, nil
}

// lockName is the lock file that serialises changes of the profile files.
const lockName = ".lock"

// ErrLocked is what LockFile returns when another holder has the lock.
var ErrLocked = errors.New("locked")

// Waiting for the lock: the holder only rewrites two small files.
var (
	lockWait = 10 * time.Second
	lockPoll = 20 * time.Millisecond
)

// lockProfiles takes the lock of Dir(). SaveProfile and DeleteProfile read
// both files, change one profile and write them back; without the lock two
// 'configure' runs at once lose one of the profiles.
func lockProfiles() (unlock func(), err error) {
	dir := Dir()
	if dir == "" {
		return nil, NoConfigDir()
	}
	if err := MkdirPrivate(dir); err != nil {
		return nil, fmt.Errorf(i18n.T("create %s: %w", "создание %s: %w"), dir, err)
	}
	// An OS file lock, not a file that exists while held: it goes away
	// with the process, so there is no stale lock to guess about. Taking
	// over a lock by its age gave it away from a live holder stopped for
	// longer (R2-4).
	path := filepath.Join(dir, lockName)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf(i18n.T("lock %s: %w", "блокировка %s: %w"), path, err)
	}
	deadline := time.Now().Add(lockWait)
	for {
		err := LockFile(f)
		if err == nil {
			return func() {
				_ = UnlockFile(f)
				_ = f.Close()
			}, nil
		}
		if !errors.Is(err, ErrLocked) {
			_ = f.Close()
			return nil, fmt.Errorf(i18n.T("lock %s: %w", "блокировка %s: %w"), path, err)
		}
		if time.Now().After(deadline) {
			_ = f.Close()
			return nil, clerrors.New(clerrors.CodeConfigInvalid,
				i18n.Tf("profiles are locked by another %s process (%s)", "профили заблокированы другим процессом %s (%s)", AppName, path),
				i18n.Tf("Wait for the other '%s configure' to finish: the lock is released when that process exits",
					"Дождитесь окончания другого '%s configure': блокировка снимается, когда тот процесс завершится", AppName))
		}
		time.Sleep(lockPoll)
	}
}

// SaveProfile writes the profile into both files (0600, directory 0700),
// keeping the other profiles.
func SaveProfile(name string, p Profile, c Credentials) error {
	if err := ValidateProfileName(name); err != nil {
		return err
	}
	unlock, err := lockProfiles()
	if err != nil {
		return err
	}
	defer unlock()
	s, err := readSettingsFile()
	if err != nil {
		return err
	}
	cr, _, err := readCredentialsFile()
	if err != nil {
		return err
	}
	if s.Profiles == nil {
		s.Profiles = map[string]Profile{}
	}
	if cr.Profiles == nil {
		cr.Profiles = map[string]Credentials{}
	}
	s.Profiles[name] = p
	cr.Profiles[name] = c
	if err := writeYAML(SettingsPath(), s); err != nil {
		return err
	}
	return writeYAML(CredentialsPath(), cr)
}

// DeleteProfile removes a profile from both files and drops its token cache.
func DeleteProfile(name string) error {
	unlock, err := lockProfiles()
	if err != nil {
		return err
	}
	defer unlock()
	s, err := readSettingsFile()
	if err != nil {
		return err
	}
	cr, _, err := readCredentialsFile()
	if err != nil {
		return err
	}
	_, ok1 := s.Profiles[name]
	_, ok2 := cr.Profiles[name]
	if !ok1 && !ok2 {
		return clerrors.New(clerrors.CodeConfigInvalid,
			i18n.Tf("profile %q not found", "профиль %q не найден", name),
			i18n.Tf("Run '%s configure list' to see existing profiles", "Список профилей: '%s configure list'", AppName))
	}
	delete(s.Profiles, name)
	delete(cr.Profiles, name)
	if err := writeYAML(SettingsPath(), s); err != nil {
		return err
	}
	if err := writeYAML(CredentialsPath(), cr); err != nil {
		return err
	}
	// A name read from the file is not validated: "x/../../y" would make
	// the cache path point outside the cache directory. Such a profile
	// never had a cache file (Resolve rejects the name).
	if ValidateProfileName(name) != nil {
		return nil
	}
	if path := TokenCachePath(name); path == "" {
		return nil
	} else if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf(i18n.T("remove token cache: %w", "удаление кэша токена: %w"), err)
	}
	return nil
}

// ProfileSummary describes a stored profile without its secrets.
type ProfileSummary struct {
	Name      string `json:"name"`
	ProjectID string `json:"project_id"`
	KeyID     string `json:"key_id"` // masked
	HasSecret bool   `json:"has_secret"`
	HasS3     bool   `json:"has_s3"`
}

// Profiles lists the stored profiles sorted by name.
func Profiles() ([]ProfileSummary, error) {
	s, err := readSettingsFile()
	if err != nil {
		return nil, err
	}
	cr, _, err := readCredentialsFile()
	if err != nil {
		return nil, err
	}
	names := map[string]bool{}
	for n := range s.Profiles {
		names[n] = true
	}
	for n := range cr.Profiles {
		names[n] = true
	}
	out := make([]ProfileSummary, 0, len(names))
	for n := range names {
		c := cr.Profiles[n]
		out = append(out, ProfileSummary{
			Name:      n,
			ProjectID: s.Profiles[n].ProjectID,
			KeyID:     MaskKeyID(c.KeyID),
			HasSecret: c.Secret != "",
			HasS3:     c.S3 != nil && c.S3.AccessKey != "",
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// writeYAML replaces path atomically with mode 0600 in a 0700 directory.
func writeYAML(path string, v any) error {
	if path == "" {
		return NoConfigDir()
	}
	data, err := yaml.Marshal(v)
	if err != nil {
		return err
	}
	return WriteFilePrivate(path, data)
}

// WriteFilePrivate writes data to path via a temporary file and rename, so
// a crash never leaves a half-written file, and the secret is never
// readable by others even for a moment.
func WriteFilePrivate(path string, data []byte) error {
	return WriteFileAtomic(path, data, 0o600)
}

// WriteFileAtomic is WriteFilePrivate with the final mode perm. The
// temporary file is created 0600 (O_EXCL) and gets perm before the rename;
// the rename replaces a symlink at path instead of writing through it, and
// a file already at path never lends its own mode to the new contents.
// Directories it creates are private (MkdirPrivate). On Windows a file with
// a private perm (no group/other bits) is created with a DACL for the
// current user, SYSTEM and Administrators only, without inheritance; a
// wider perm keeps the inherited ACL.
func WriteFileAtomic(path string, data []byte, perm fs.FileMode) error {
	dir := filepath.Dir(path)
	if err := MkdirPrivate(dir); err != nil {
		return fmt.Errorf(i18n.T("create %s: %w", "создание %s: %w"), dir, err)
	}
	createTemp := os.CreateTemp
	if !LooseMode(perm) {
		createTemp = createPrivateTemp
	}
	tmp, err := createTemp(dir, "."+filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf(i18n.T("write %s: %w", "запись %s: %w"), path, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after a successful rename
	if err := tmp.Chmod(perm); err != nil && PermissionsEnforced() {
		tmp.Close()
		return fmt.Errorf(i18n.T("chmod %s: %w", "смена прав %s: %w"), tmpName, err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf(i18n.T("write %s: %w", "запись %s: %w"), path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf(i18n.T("write %s: %w", "запись %s: %w"), path, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf(i18n.T("write %s: %w", "запись %s: %w"), path, err)
	}
	return nil
}
