// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

// Package config resolves the configuration of evo from, highest
// precedence first: CLI flags > environment > project file
// (./.evo.yaml) > profile ($CONFIG/evo) > defaults.
package config

import (
	stderrors "errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// Config holds all configuration values for evo.
type Config struct {
	// Profile is the name of the profile the values were read from.
	Profile string `yaml:"-" json:"profile"`

	// Auth. Never read from the project file: secrets live in
	// credentials.yaml or the environment only.
	KeyID  string `yaml:"-" json:"key_id"`
	Secret string `yaml:"-" json:"-"`
	// KeyEnv names the variables the key came from when they are not
	// CLOUDRU_KEY_ID / CLOUDRU_SECRET (CLOUDRU_KEY_SECRET, EVOLUTION_*):
	// evo doctor shows them, so it is clear which key went to IAM.
	KeyEnv string `yaml:"-" json:"-"`

	// Deploy
	ProjectID string `yaml:"project_id" json:"project_id"` // required UUID
	// ProjectOrigin is where ProjectID came from (OriginProfile, …): the
	// previews name it, since with a key that sees several projects the
	// project is a setting the user may not have chosen on purpose.
	ProjectOrigin string `yaml:"-" json:"-"`
	Region        string `yaml:"region" json:"region"` // availability_zone_id UUID
	Flavor        string `yaml:"flavor" json:"flavor"`
	ImageID       string `yaml:"image_id" json:"image_id"` // boot image UUID
	// Image is the boot image by catalogue reference: a name, a display
	// name or a UUID, resolved by images.Resolve (image: in .evo.yaml,
	// --image). With ImageID the one from the higher layer wins; both in
	// one layer is an error.
	Image string `yaml:"image" json:"image,omitempty"`
	// ImageMetadata are explicit values of the image's metadata fields
	// (image_metadata: in .evo.yaml): what the image needs and evo cannot
	// fill itself. Project file only.
	ImageMetadata map[string]string `yaml:"image_metadata" json:"-"`
	DiskSize      int               `yaml:"disk_size" json:"disk_size"`
	Timeout       int               `yaml:"timeout" json:"timeout"` // seconds deploy waits for the VM to become active
	// SecurityGroup is the group (name or UUID) deploy puts the VM in and
	// opens the compose ports in; empty — chosen by resolveNetwork.
	SecurityGroup string `yaml:"security_group" json:"security_group,omitempty"`

	// Output
	JSON    bool `yaml:"-" json:"-"` // --json flag only
	Verbose bool `yaml:"-" json:"-"` // --verbose flag only
	NoColor bool `yaml:"-" json:"-"` // --no-color flag only

	// Project
	ComposeFile string `yaml:"compose_file" json:"compose_file"`
	ProjectName string `yaml:"project_name" json:"project_name"`

	// S3 holds the Object Storage credentials (optional profile section).
	S3 S3Config `yaml:"-" json:"-"`

	// Warnings are non-fatal problems found while resolving (deprecated
	// variables, secrets in the project file, loose file permissions).
	// The caller prints them to stderr.
	Warnings []string `yaml:"-" json:"-"`
}

// S3Config holds Cloud.ru Object Storage settings.
type S3Config struct {
	Endpoint  string `yaml:"endpoint,omitempty"`
	AccessKey string `yaml:"access_key,omitempty"`
	SecretKey string `yaml:"secret_key,omitempty"`
	Region    string `yaml:"region,omitempty"`
}

// Origins of a value, as reported by ResolveWithSources.
const (
	OriginDefault = "default"
	OriginProfile = "profile"
	OriginProject = "project"
	OriginEnv     = "env"
	OriginFlag    = "flag"
)

// Source tracks where a config value came from (for the doctor command).
// Secret values are masked.
type Source struct {
	Key    string
	Value  any
	Origin string
}

// DefaultProfile is used when neither --profile nor CLOUDRU_PROFILE is set.
const DefaultProfile = "default"

// Options select the inputs of Resolve.
type Options struct {
	// Profile is the --profile flag value; "" falls back to CLOUDRU_PROFILE,
	// then to DefaultProfile. A profile named explicitly must exist.
	Profile string
	// ProjectFile is the path of ./.evo.yaml; "" or a missing file
	// means no project overrides.
	ProjectFile string
	// Flags maps Config field names to values set via CLI flags.
	Flags map[string]any
}

// Defaults returns a Config with sensible default values.
func Defaults() Config {
	return Config{
		Flavor:      "auto",
		DiskSize:    10,
		Timeout:     600,
		ComposeFile: "docker-compose.yml",
	}
}

// ResolveWithSources returns the config plus the origin of each value.
func ResolveWithSources(opts Options) (Config, []Source, error) {
	cfg, origins, err := resolve(opts)
	if err != nil {
		return Config{}, nil, err
	}
	return cfg, buildSources(&cfg, origins), nil
}

// ProfileName returns the profile selected by flag, CLOUDRU_PROFILE or the
// default, and whether it was chosen explicitly.
func ProfileName(flag string) (name string, explicit bool) {
	if flag != "" {
		return flag, true
	}
	if env := os.Getenv("CLOUDRU_PROFILE"); env != "" {
		return env, true
	}
	return DefaultProfile, false
}

// envMapping maps env var names to config field names (non-credential).
var envMapping = []struct {
	envVar string
	field  string
}{
	// AI Agents passes the project as EVOLUTION_PROJECT_ID (docs «Переменные
	// MCP-сервера»); CLOUDRU_PROJECT_ID, applied later, wins.
	{"EVOLUTION_PROJECT_ID", "ProjectID"},
	{"CLOUDRU_PROJECT_ID", "ProjectID"},
	{"CLOUDRU_REGION", "Region"},
	{"CLOUDRU_FLAVOR", "Flavor"},
	{"CLOUDRU_IMAGE_ID", "ImageID"},
	{"CLOUDRU_DISK_SIZE", "DiskSize"},
	{"CLOUDRU_TIMEOUT", "Timeout"},
	{"CLOUDRU_COMPOSE_FILE", "ComposeFile"},
	{"CLOUDRU_PROJECT_NAME", "ProjectName"},
	{"CLOUDRU_S3_ENDPOINT", "S3Endpoint"},
	{"CLOUDRU_S3_ACCESS_KEY", "S3AccessKey"},
	{"CLOUDRU_S3_SECRET_KEY", "S3SecretKey"},
	{"CLOUDRU_S3_REGION", "S3Region"},
}

func resolve(opts Options) (Config, map[string]string, error) {
	cfg := Defaults()
	origins := map[string]string{}

	name, explicit := ProfileName(opts.Profile)
	if err := ValidateProfileName(name); err != nil {
		return Config{}, nil, err
	}
	cfg.Profile = name

	// Layer 1: profile from $CONFIG/evo.
	if err := applyProfile(&cfg, name, explicit, origins); err != nil {
		return Config{}, nil, err
	}

	profileProject := ""
	if origins["ProjectID"] == OriginProfile {
		profileProject = cfg.ProjectID
	}

	// Layer 2: project file.
	if opts.ProjectFile != "" {
		if err := applyProjectFile(&cfg, opts.ProjectFile, origins); err != nil {
			return Config{}, nil, err
		}
	}

	// Layer 3: env vars. An empty variable counts as unset.
	for _, m := range envMapping {
		val := os.Getenv(m.envVar)
		if val == "" {
			continue
		}
		if err := setField(&cfg, m.field, val); err != nil {
			return Config{}, nil, clerrors.Wrap(clerrors.CodeConfigInvalid,
				i18n.Tf("env %s: %s", "переменная %s: %s", m.envVar, errText(err)),
				i18n.T("Fix or unset the variable", "Исправьте или удалите переменную"), err)
		}
		origins[m.field] = OriginEnv
	}
	if err := applyEnvCredentials(&cfg, origins); err != nil {
		return Config{}, nil, err
	}

	// .evo.yaml or CLOUDRU_PROJECT_ID silently taking over the profile's
	// project is how a command lands in the wrong project: say so. A flag
	// is explicit and needs no warning.
	if profileProject != "" && cfg.ProjectID != profileProject {
		from := ProjectFileName
		if origins["ProjectID"] == OriginEnv {
			from = "CLOUDRU_PROJECT_ID"
			if os.Getenv("CLOUDRU_PROJECT_ID") == "" {
				from = "EVOLUTION_PROJECT_ID"
			}
		}
		if _, flagged := opts.Flags["ProjectID"]; !flagged {
			cfg.Warnings = append(cfg.Warnings, i18n.Tf(
				"project %s from %s overrides project %s of profile %q",
				"проект %s из %s заменяет проект %s профиля %q",
				cfg.ProjectID, from, profileProject, name))
		}
	}

	// Layer 4: CLI flags (highest precedence).
	for key, val := range opts.Flags {
		if err := setFieldAny(&cfg, key, val); err != nil {
			// A refusal with its own code (USAGE: --disk-size -5) says
			// what is wrong in the flag's own terms.
			var ce *clerrors.CLIError
			if stderrors.As(err, &ce) {
				return Config{}, nil, err
			}
			return Config{}, nil, fmt.Errorf("flag %s: %w", key, err)
		}
		origins[key] = OriginFlag
	}
	cfg.ProjectOrigin = origins["ProjectID"]
	warnAgentProject(&cfg, origins)
	if err := pickImage(&cfg, origins); err != nil {
		return Config{}, nil, err
	}

	return cfg, origins, nil
}

// originRank orders the layers: a higher one overrides a lower one.
var originRank = map[string]int{OriginDefault: 0, OriginProfile: 1, OriginProject: 2, OriginEnv: 3, OriginFlag: 4}

// pickImage keeps one of image_id and image, the two ways to name the boot
// image: the one set in the higher layer (image: in .evo.yaml overrides the
// profile's image_id, --image-id overrides image:). Both in the same layer
// would leave the choice to evo, so that is an error.
func pickImage(cfg *Config, origins map[string]string) error {
	if cfg.ImageID == "" || cfg.Image == "" {
		return nil
	}
	idRank, refRank := originRank[origins["ImageID"]], originRank[origins["Image"]]
	switch {
	case idRank > refRank:
		cfg.Image = ""
		delete(origins, "Image")
	case refRank > idRank:
		cfg.ImageID = ""
		delete(origins, "ImageID")
	default:
		return clerrors.New(clerrors.CodeConfigInvalid,
			i18n.Tf("both image_id (%s) and image (%s) are set", "заданы и image_id (%s), и image (%s)", cfg.ImageID, cfg.Image),
			i18n.T("Keep one of them: image takes a name or a UUID, image_id a UUID.",
				"Оставьте одно: image — имя или UUID образа, image_id — только UUID."))
	}
	return nil
}

// applyEnvCredentials takes the key pair from the environment. The pair is
// taken as a whole: mixing an env key ID with a profile secret would only
// produce a confusing IAM rejection. The secret is CLOUDRU_SECRET, else
// CLOUDRU_KEY_SECRET — the name of the Cloud.ru MCP guide (Container Apps,
// guides__mcp-server), whose env block colleagues copy. Without either
// CLOUDRU_ variable the pair AI Agents passes to an MCP server is taken:
// EVOLUTION_SERVICE_ACCOUNT_KEY_ID and _SECRET.
func applyEnvCredentials(cfg *Config, origins map[string]string) error {
	// Values pasted from a .env file often carry "\r" or spaces; IAM would
	// reject the key with no hint why.
	keyID := strings.TrimSpace(os.Getenv("CLOUDRU_KEY_ID"))
	secret := strings.TrimSpace(os.Getenv("CLOUDRU_SECRET"))
	if secret == "" {
		secret = strings.TrimSpace(os.Getenv("CLOUDRU_KEY_SECRET"))
	}
	switch {
	case keyID == "" && secret == "":
		keyID = strings.TrimSpace(os.Getenv("EVOLUTION_SERVICE_ACCOUNT_KEY_ID"))
		secret = strings.TrimSpace(os.Getenv("EVOLUTION_SERVICE_ACCOUNT_KEY_SECRET"))
		if keyID != "" || secret != "" {
			cfg.KeyEnv = "EVOLUTION_SERVICE_ACCOUNT_KEY_ID, EVOLUTION_SERVICE_ACCOUNT_KEY_SECRET"
		}
	case secret != "" && strings.TrimSpace(os.Getenv("CLOUDRU_SECRET")) == "":
		cfg.KeyEnv = "CLOUDRU_KEY_ID, CLOUDRU_KEY_SECRET"
	}
	if keyID != "" || secret != "" {
		cfg.KeyID, cfg.Secret = keyID, secret
		origins["KeyID"], origins["Secret"] = OriginEnv, OriginEnv
		return nil
	}
	legacy := strings.TrimSpace(os.Getenv("CLOUDRU_API_KEY"))
	if legacy == "" {
		return nil
	}
	id, sec, ok := splitLegacyKey(legacy)
	if !ok {
		return clerrors.New(clerrors.CodeAuthRequired,
			i18n.T("CLOUDRU_API_KEY must be in format 'keyId:secret'", "CLOUDRU_API_KEY должен иметь вид 'keyId:secret'"),
			i18n.T("Set CLOUDRU_KEY_ID and CLOUDRU_SECRET instead, or run 'evo configure'",
				"Вместо неё задайте CLOUDRU_KEY_ID и CLOUDRU_SECRET или выполните 'evo configure'"))
	}
	cfg.KeyID, cfg.Secret = id, sec
	origins["KeyID"], origins["Secret"] = OriginEnv, OriginEnv
	cfg.Warnings = append(cfg.Warnings,
		i18n.T("CLOUDRU_API_KEY=keyId:secret is deprecated: set CLOUDRU_KEY_ID and CLOUDRU_SECRET, or run 'evo configure'",
			"CLOUDRU_API_KEY=keyId:secret устарел: задайте CLOUDRU_KEY_ID и CLOUDRU_SECRET или выполните 'evo configure'"))
	return nil
}

// warnAgentProject: the key of an AI Agents service account
// (EVOLUTION_SERVICE_ACCOUNT_KEY_*) with a project other than the one the
// platform passed (EVOLUTION_PROJECT_ID) — a flag, CLOUDRU_PROJECT_ID,
// .evo.yaml or the profile — lands resources in a project the agent did
// not expect, or answers 403. Say where the project came from.
func warnAgentProject(cfg *Config, origins map[string]string) {
	agentProject := strings.TrimSpace(os.Getenv("EVOLUTION_PROJECT_ID"))
	if !strings.HasPrefix(cfg.KeyEnv, "EVOLUTION_") || agentProject == "" || cfg.ProjectID == agentProject {
		return
	}
	from := origins["ProjectID"]
	switch from {
	case OriginEnv:
		from = "CLOUDRU_PROJECT_ID"
	case OriginProject:
		from = ProjectFileName
	case OriginFlag:
		from = "--project-id"
	}
	cfg.Warnings = append(cfg.Warnings, i18n.Tf(
		"the key is from EVOLUTION_SERVICE_ACCOUNT_KEY_*, the project %s from %s, not EVOLUTION_PROJECT_ID (%s)",
		"ключ — из EVOLUTION_SERVICE_ACCOUNT_KEY_*, а проект %s — из %s, не из EVOLUTION_PROJECT_ID (%s)",
		cfg.ProjectID, from, agentProject))
}

func splitLegacyKey(v string) (keyID, secret string, ok bool) {
	keyID, secret, found := strings.Cut(v, ":")
	keyID, secret = strings.TrimSpace(keyID), strings.TrimSpace(secret)
	return keyID, secret, found && keyID != "" && secret != ""
}

// applyProfile merges config.yaml and credentials.yaml entries of profile.
func applyProfile(cfg *Config, name string, explicit bool, origins map[string]string) error {
	settings, err := readSettingsFile()
	if err != nil {
		return err
	}
	creds, credWarn, err := readCredentialsFile()
	if err != nil {
		return err
	}
	cfg.Warnings = append(cfg.Warnings, credWarn...)

	p, hasSettings := settings.Profiles[name]
	c, hasCreds := creds.Profiles[name]
	if !hasSettings && !hasCreds {
		if explicit {
			if Dir() == "" {
				return NoConfigDir()
			}
			return clerrors.New(clerrors.CodeConfigInvalid,
				i18n.Tf("profile %q not found in %s", "профиль %q не найден в %s", name, Dir()),
				i18n.Tf("Run 'evo configure --profile %s' to create it, or 'evo configure list' to see existing profiles",
					"Создайте его: 'evo configure --profile %s'; список профилей — 'evo configure list'", name))
		}
		return nil
	}

	set := func(field string, ok bool) {
		if ok {
			origins[field] = OriginProfile
		}
	}
	if p.ProjectID != "" {
		cfg.ProjectID = p.ProjectID
	}
	set("ProjectID", p.ProjectID != "")
	if p.Region != "" {
		cfg.Region = p.Region
	}
	set("Region", p.Region != "")
	if p.Flavor != "" {
		cfg.Flavor = p.Flavor
	}
	set("Flavor", p.Flavor != "")
	if p.ImageID != "" {
		cfg.ImageID = p.ImageID
	}
	set("ImageID", p.ImageID != "")
	if p.DiskSize != 0 {
		cfg.DiskSize = p.DiskSize
	}
	set("DiskSize", p.DiskSize != 0)
	if p.Timeout != 0 {
		cfg.Timeout = p.Timeout
	}
	set("Timeout", p.Timeout != 0)

	if c.KeyID != "" || c.Secret != "" {
		cfg.KeyID, cfg.Secret = c.KeyID, c.Secret
		origins["KeyID"], origins["Secret"] = OriginProfile, OriginProfile
	}
	if c.S3 != nil {
		mergeS3(&cfg.S3, *c.S3, origins, OriginProfile)
	}
	return nil
}

func mergeS3(dst *S3Config, src S3Config, origins map[string]string, origin string) {
	if src.Endpoint != "" {
		dst.Endpoint = src.Endpoint
		origins["S3Endpoint"] = origin
	}
	if src.AccessKey != "" {
		dst.AccessKey = src.AccessKey
		origins["S3AccessKey"] = origin
	}
	if src.SecretKey != "" {
		dst.SecretKey = src.SecretKey
		origins["S3SecretKey"] = origin
	}
	if src.Region != "" {
		dst.Region = src.Region
		origins["S3Region"] = origin
	}
}

// secretKeys are keys that must not be in the project file: it is meant to
// be committed with the project.
var secretKeys = []string{"api_key", "key_id", "secret", "s3"}

// applyProjectFile merges ./.evo.yaml.
func applyProjectFile(cfg *Config, path string, origins map[string]string) error {
	// The file comes with the project: a FIFO there would block every
	// command run in the directory.
	if st, err := os.Stat(path); err == nil && !st.Mode().IsRegular() {
		return clerrors.New(clerrors.CodeConfigInvalid,
			i18n.Tf("%s is not a regular file", "%s — не обычный файл", path),
			i18n.T("Remove it (it may have come with the repository) and run the command again",
				"Удалите его (он мог прийти вместе с репозиторием) и повторите команду"))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return clerrors.Wrap(clerrors.CodeConfigInvalid, i18n.T("reading config file: ", "чтение файла настроек: ")+err.Error(),
			i18n.T("Check the permissions of ", "Проверьте права на ")+path, err)
	}
	var fileCfg Config
	if err := yaml.Unmarshal(data, &fileCfg); err != nil {
		return clerrors.Wrap(clerrors.CodeConfigInvalid, i18n.Tf("parsing %s: %v", "разбор %s: %v", path, err),
			i18n.T("Fix the YAML syntax", "Исправьте синтаксис YAML"), err)
	}
	var raw map[string]any
	_ = yaml.Unmarshal(data, &raw)
	for _, k := range secretKeys {
		if _, ok := raw[k]; ok {
			cfg.Warnings = append(cfg.Warnings, i18n.Tf(
				"%s: %q is ignored, secrets are not read from the project file; run 'evo configure' and remove it",
				"%s: %q не учитывается — секреты из файла проекта не читаются; выполните 'evo configure' и удалите ключ из файла", path, k))
		}
	}

	mergeString := func(dst *string, v, field string) {
		if v != "" {
			*dst = v
			origins[field] = OriginProject
		}
	}
	mergeString(&cfg.ProjectID, fileCfg.ProjectID, "ProjectID")
	mergeString(&cfg.Region, fileCfg.Region, "Region")
	mergeString(&cfg.Flavor, fileCfg.Flavor, "Flavor")
	mergeString(&cfg.ImageID, fileCfg.ImageID, "ImageID")
	mergeString(&cfg.Image, strings.TrimSpace(fileCfg.Image), "Image")
	if len(fileCfg.ImageMetadata) > 0 {
		cfg.ImageMetadata = fileCfg.ImageMetadata
		origins["ImageMetadata"] = OriginProject
	}
	mergeString(&cfg.ComposeFile, fileCfg.ComposeFile, "ComposeFile")
	mergeString(&cfg.ProjectName, fileCfg.ProjectName, "ProjectName")
	// security_group: was parsed and never merged: deploy opened the
	// compose ports in the zone's default group (release audit, A-001).
	mergeString(&cfg.SecurityGroup, strings.TrimSpace(fileCfg.SecurityGroup), "SecurityGroup")
	if err := provision.CheckBootDiskGB(fileCfg.DiskSize); err != nil {
		return clerrors.Wrap(clerrors.CodeConfigInvalid,
			i18n.Tf("%s: disk_size must be a positive number of GB, not %d", "%s: disk_size — положительное число ГБ, а не %d", path, fileCfg.DiskSize),
			i18n.T("Set disk_size to the size in GB, or remove it for the default.", "Задайте в disk_size размер в ГБ или удалите ключ: будет размер по умолчанию."), err)
	}
	if fileCfg.DiskSize != 0 {
		cfg.DiskSize = fileCfg.DiskSize
		origins["DiskSize"] = OriginProject
	}
	if fileCfg.Timeout != 0 {
		cfg.Timeout = fileCfg.Timeout
		origins["Timeout"] = OriginProject
	}
	return nil
}

// errText is the message of err without the code prefix a CLIError
// carries ("USAGE: …").
func errText(err error) string {
	var ce *clerrors.CLIError
	if stderrors.As(err, &ce) {
		return ce.Message
	}
	return err.Error()
}

// setField sets a Config field from a string value (used for env vars).
func setField(cfg *Config, field, val string) error {
	switch field {
	case "DiskSize", "Timeout":
		n, err := strconv.Atoi(val)
		if err != nil {
			return fmt.Errorf(i18n.T("invalid integer: %w", "не целое число: %w"), err)
		}
		return setFieldAny(cfg, field, n)
	}
	return setFieldAny(cfg, field, val)
}

// setFieldAny sets a Config field from an any value (used for flags).
func setFieldAny(cfg *Config, field string, val any) error {
	switch field {
	case "ProjectID":
		cfg.ProjectID = toString(val)
	case "Region":
		cfg.Region = toString(val)
	case "Flavor":
		cfg.Flavor = toString(val)
	case "ImageID":
		cfg.ImageID = toString(val)
	case "Image":
		cfg.Image = strings.TrimSpace(toString(val))
	case "DiskSize":
		n, err := toInt(val)
		if err != nil {
			return err
		}
		if err := provision.CheckBootDiskGB(n); err != nil {
			return err
		}
		cfg.DiskSize = n
	case "Timeout":
		n, err := toInt(val)
		if err != nil {
			return err
		}
		cfg.Timeout = n
	case "ComposeFile":
		cfg.ComposeFile = toString(val)
	case "ProjectName":
		cfg.ProjectName = toString(val)
	case "S3Endpoint":
		cfg.S3.Endpoint = toString(val)
	case "S3AccessKey":
		cfg.S3.AccessKey = toString(val)
	case "S3SecretKey":
		cfg.S3.SecretKey = toString(val)
	case "S3Region":
		cfg.S3.Region = toString(val)
	case "JSON":
		cfg.JSON = toBool(val)
	case "Verbose":
		cfg.Verbose = toBool(val)
	case "NoColor":
		cfg.NoColor = toBool(val)
	default:
		return fmt.Errorf("unknown field %q", field)
	}
	return nil
}

func toString(v any) string {
	switch s := v.(type) {
	case string:
		return s
	default:
		return fmt.Sprintf("%v", v)
	}
}

func toInt(v any) (int, error) {
	switch n := v.(type) {
	case int:
		return n, nil
	case int64:
		return int(n), nil
	case float64:
		return int(n), nil
	case string:
		return strconv.Atoi(n)
	default:
		return 0, fmt.Errorf("cannot convert %T to int", v)
	}
}

func toBool(v any) bool {
	switch b := v.(type) {
	case bool:
		return b
	case string:
		return b == "true" || b == "1"
	default:
		return false
	}
}

// MaskKeyID hides the middle of a key ID: enough to recognise it, not
// enough to reuse it.
func MaskKeyID(s string) string {
	if s == "" {
		return ""
	}
	if len(s) <= 8 {
		return "****"
	}
	return s[:4] + "****" + s[len(s)-4:]
}

func maskSecret(s string) string {
	if s == "" {
		return ""
	}
	return "****"
}

// buildSources lists the resolved values with their origins.
func buildSources(cfg *Config, origins map[string]string) []Source {
	fields := []struct {
		key string
		val any
	}{
		{"KeyID", MaskKeyID(cfg.KeyID)},
		{"Secret", maskSecret(cfg.Secret)},
		{"ProjectID", cfg.ProjectID},
		{"Region", cfg.Region},
		{"Flavor", cfg.Flavor},
		{"ImageID", cfg.ImageID},
		{"Image", cfg.Image},
		{"DiskSize", cfg.DiskSize},
		{"Timeout", cfg.Timeout},
		{"ComposeFile", cfg.ComposeFile},
		{"ProjectName", cfg.ProjectName},
		{"SecurityGroup", cfg.SecurityGroup},
		{"S3Endpoint", cfg.S3.Endpoint},
		{"S3AccessKey", MaskKeyID(cfg.S3.AccessKey)},
		{"S3SecretKey", maskSecret(cfg.S3.SecretKey)},
		{"S3Region", cfg.S3.Region},
	}
	sources := make([]Source, 0, len(fields))
	for _, f := range fields {
		origin := origins[f.key]
		if origin == "" {
			origin = OriginDefault
		}
		if (f.key == "KeyID" || f.key == "Secret") && origin == OriginEnv && cfg.KeyEnv != "" {
			origin += " (" + cfg.KeyEnv + ")"
		}
		sources = append(sources, Source{Key: f.key, Value: f.val, Origin: origin})
	}
	return sources
}
