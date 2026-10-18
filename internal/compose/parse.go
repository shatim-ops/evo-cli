// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package compose

import (
	"crypto/sha256"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"

	clierr "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"gopkg.in/yaml.v3"
)

// Parse reads a docker-compose.yml file at path and returns a populated Project.
func Parse(path string) (*Project, error) {
	// A FIFO or a device would block ReadFile until someone writes to it,
	// whatever the caller's context: an MCP deploy would hold its slot for
	// good (R2-5). A symlink to a regular file is fine.
	if st, err := os.Stat(path); err == nil && !st.Mode().IsRegular() {
		return nil, clierr.New(clierr.CodeComposeInvalid,
			i18n.Tf("%s is not a regular file", "%s — не обычный файл", path),
			i18n.T("Point -f (compose_file) at a regular compose file.", "Укажите в -f (compose_file) обычный compose-файл."))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, clierr.Wrap(
				clierr.CodeComposeNotFound,
				i18n.Tf("compose file not found: %s", "compose-файл не найден: %s", path),
				i18n.T("Check the file path and try again.", "Проверьте путь (по умолчанию docker-compose.yml; другой — флаг -f) и повторите."),
				err,
			)
		}
		return nil, clierr.Wrap(
			clierr.CodeComposeNotFound,
			i18n.Tf("cannot read compose file: %s", "не удалось прочитать compose-файл: %s", path),
			i18n.T("Check file permissions.", "Проверьте права на файл."),
			err,
		)
	}
	return parseData(data)
}

// parseData parses the content of a compose file.
func parseData(data []byte) (*Project, error) {
	hash := fmt.Sprintf("%x", sha256.Sum256(data))

	var root map[string]interface{}
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, clierr.Wrap(
			clierr.CodeComposeInvalid,
			i18n.T("invalid YAML in compose file", "в compose-файле ошибка YAML"),
			i18n.T("Ensure the file contains valid YAML.", "Проверьте синтаксис YAML."),
			err,
		)
	}

	servicesRaw, ok := root["services"]
	if !ok {
		return nil, clierr.New(
			clierr.CodeComposeInvalid,
			noServices(),
			addServices(),
		)
	}

	servicesMap, ok := servicesRaw.(map[string]interface{})
	if !ok || len(servicesMap) == 0 {
		return nil, clierr.New(
			clierr.CodeComposeInvalid,
			noServices(),
			addServices(),
		)
	}

	// Sort service names for deterministic output.
	names := make([]string, 0, len(servicesMap))
	for name := range servicesMap {
		names = append(names, name)
	}
	sort.Strings(names)

	var (
		services []Service
		warnings []string
		portSet  = make(map[int]struct{})
		udpSet   = make(map[int]struct{})
		totalCPU float64
		totalMem int
	)

	for _, name := range names {
		svcRaw := servicesMap[name]
		svcMap, ok := svcRaw.(map[string]interface{})
		if !ok {
			continue
		}

		svc := Service{Name: name}

		if img, ok := svcMap["image"].(string); ok {
			svc.Image = img
		}

		ports, warns, err := parsePorts(svcMap)
		if err != nil {
			return nil, clierr.New(clierr.CodeComposeInvalid,
				i18n.Tf("service %q: %v", "сервис %q: %v", name, err),
				i18n.T("Ports are [IP:][HOST:]CONTAINER[/tcp|udp], each port 1-65535.",
					"Порт задаётся как [IP:][HOST:]CONTAINER[/tcp|udp], номер — от 1 до 65535."))
		}
		svc.Ports = ports
		for _, w := range warns {
			warnings = append(warnings, i18n.Tf("service %q: %s", "сервис %q: %s", name, w))
		}
		for _, p := range svc.Ports {
			if !p.Public() {
				continue
			}
			if p.Protocol == "udp" {
				udpSet[p.Host] = struct{}{}
			} else if p.Protocol == "tcp" {
				portSet[p.Host] = struct{}{}
			}
		}

		svc.Resources, err = parseResources(svcMap)
		if err != nil {
			return nil, clierr.New(clierr.CodeComposeInvalid,
				i18n.Tf("service %q: %v", "сервис %q: %v", name, err),
				i18n.T("Use deploy.resources.limits like cpus: \"0.5\" and memory: 512M.",
					"Задайте deploy.resources.limits, например cpus: \"0.5\" и memory: 512M."))
		}
		totalCPU += svc.Resources.CPUCores
		totalMem += svc.Resources.MemoryMB

		svc.DependsOn = parseDependsOn(svcMap)
		for _, w := range localOnly(svcMap) {
			warnings = append(warnings, i18n.Tf("service %q: %s", "сервис %q: %s", name, w))
		}

		services = append(services, svc)
	}
	warnings = append(warnings, fileSources(root)...)

	if len(services) == 0 {
		return nil, clierr.New(
			clierr.CodeComposeInvalid,
			i18n.T("no valid services found in compose file", "в compose-файле нет ни одного корректного сервиса"),
			i18n.T("Ensure services are properly defined.", "Проверьте описание сервисов."),
		)
	}

	return &Project{
		Services:  services,
		Ports:     sortedKeys(portSet),
		UDPPorts:  sortedKeys(udpSet),
		Resources: Resources{CPUCores: totalCPU, MemoryMB: totalMem},
		Hash:      hash,
		Warnings:  warnings,
	}, nil
}

func noServices() string {
	return i18n.T("no services defined in compose file", "в compose-файле нет сервисов")
}

func addServices() string {
	return i18n.T("Add a 'services' section to your docker-compose.yml.", "Добавьте в compose-файл раздел 'services'.")
}

func sortedKeys(set map[int]struct{}) []int {
	if len(set) == 0 {
		return nil
	}
	out := make([]int, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Ints(out)
	return out
}

// parsePorts extracts port mappings from a service definition, short
// ("[IP:][HOST:]CONTAINER[/PROTO]", ranges "8000-8002:80-82") and long
// syntax. Ports Docker would reject are an error; a host port that depends
// on a variable the VM does not have is a warning.
func parsePorts(svc map[string]interface{}) ([]PortMapping, []string, error) {
	portsRaw, ok := svc["ports"]
	if !ok || portsRaw == nil {
		return nil, nil, nil
	}
	portsList, ok := portsRaw.([]interface{})
	if !ok {
		return nil, nil, fmt.Errorf("ports must be a list")
	}

	var (
		mappings []PortMapping
		warnings []string
	)
	for _, p := range portsList {
		var (
			ms  []PortMapping
			err error
		)
		switch v := p.(type) {
		case string:
			s, unset := interpolate(v)
			if len(unset) > 0 {
				warnings = append(warnings, i18n.Tf(
					"port %q uses %s, which is not set on the VM (deploy uploads only the compose file); the port is not opened",
					"порт %q использует %s, а на ВМ этой переменной нет (deploy загружает только compose-файл); порт не открывается",
					v, strings.Join(unset, ", ")))
				continue
			}
			ms, err = parsePortString(s)
		case int:
			ms, err = parsePortString(strconv.Itoa(v))
		case map[string]interface{}:
			ms, err = parseLongPort(v)
		default:
			err = fmt.Errorf("unsupported port entry %v", v)
		}
		if err != nil {
			return nil, nil, err
		}
		mappings = append(mappings, ms...)
	}
	return mappings, warnings, nil
}

// interpolate resolves ${VAR...} the way 'docker compose up' on the VM does:
// there no variable from the user's shell or .env exists, so only defaults
// apply. It returns the variables used without a default.
func interpolate(s string) (string, []string) {
	if !strings.Contains(s, "$") {
		return s, nil
	}
	var (
		out   strings.Builder
		unset []string
	)
	for i := 0; i < len(s); i++ {
		if s[i] != '$' {
			out.WriteByte(s[i])
			continue
		}
		if i+1 < len(s) && s[i+1] == '$' {
			out.WriteByte('$')
			i++
			continue
		}
		if i+1 < len(s) && s[i+1] == '{' {
			end := strings.IndexByte(s[i:], '}')
			if end < 0 {
				return s, []string{s[i:]}
			}
			expr := s[i+2 : i+end]
			i += end
			k := nameLen(expr)
			name, op := expr[:k], expr[k:]
			switch {
			case strings.HasPrefix(op, ":-"):
				out.WriteString(op[2:])
			case strings.HasPrefix(op, "-"):
				out.WriteString(op[1:])
			default: // ${VAR}, ${VAR:?err}, ${VAR:+alt}: no value on the VM
				unset = append(unset, "${"+name+"}")
			}
			continue
		}
		j := i + 1 + nameLen(s[i+1:])
		unset = append(unset, "$"+s[i+1:j])
		i = j - 1
	}
	return out.String(), unset
}

// nameLen is the length of the variable name s starts with.
func nameLen(s string) int {
	n := 0
	for n < len(s) && (s[n] == '_' || s[n] >= 'a' && s[n] <= 'z' || s[n] >= 'A' && s[n] <= 'Z' || s[n] >= '0' && s[n] <= '9') {
		n++
	}
	return n
}

// parsePortString parses the short syntax into one mapping per port.
func parsePortString(s string) ([]PortMapping, error) {
	orig := s
	proto := "tcp"
	if idx := strings.LastIndex(s, "/"); idx != -1 {
		proto = strings.ToLower(strings.TrimSpace(s[idx+1:]))
		s = s[:idx]
	}
	if proto != "tcp" && proto != "udp" && proto != "sctp" {
		return nil, fmt.Errorf("port %q: unknown protocol %q", orig, proto)
	}
	s = strings.TrimSpace(s)

	hostIP := ""
	if strings.HasPrefix(s, "[") { // [::1]:8080:80
		end := strings.Index(s, "]")
		if end < 0 || end+1 >= len(s) || s[end+1] != ':' {
			return nil, fmt.Errorf("port %q: bad IPv6 address", orig)
		}
		hostIP, s = s[1:end], s[end+2:]
		if !strings.Contains(s, ":") {
			s = ":" + s // [::1]:80 is IP and container port
		}
	}
	parts := strings.Split(s, ":")
	var hostPart, contPart string
	switch {
	case len(parts) == 1:
		contPart = parts[0]
	case len(parts) == 2:
		hostPart, contPart = parts[0], parts[1]
	case len(parts) == 3 && hostIP == "":
		hostIP, hostPart, contPart = parts[0], parts[1], parts[2]
	default:
		return nil, fmt.Errorf("port %q: expected [IP:][HOST:]CONTAINER", orig)
	}

	cLo, cHi, err := parsePortRange(contPart)
	if err != nil {
		return nil, fmt.Errorf("port %q: %v", orig, err)
	}
	var hLo, hHi int
	if hostPart != "" {
		if hLo, hHi, err = parsePortRange(hostPart); err != nil {
			return nil, fmt.Errorf("port %q: %v", orig, err)
		}
	}

	var out []PortMapping
	switch {
	case hostPart == "": // random host port(s)
		for c := cLo; c <= cHi; c++ {
			out = append(out, PortMapping{Container: c, Protocol: proto, HostIP: hostIP})
		}
	case hHi-hLo == cHi-cLo:
		for k := 0; k <= cHi-cLo; k++ {
			out = append(out, PortMapping{Host: hLo + k, Container: cLo + k, Protocol: proto, HostIP: hostIP})
		}
	case cLo == cHi: // a host range for one container port: Docker picks one
		out = append(out, PortMapping{Container: cLo, Protocol: proto, HostIP: hostIP})
	default:
		return nil, fmt.Errorf("port %q: host and container ranges differ in size", orig)
	}
	return out, nil
}

// parsePortRange parses "80" or "8000-8010".
func parsePortRange(s string) (int, int, error) {
	s = strings.TrimSpace(s)
	lo, hi := s, s
	if k := strings.Index(s, "-"); k > 0 {
		lo, hi = s[:k], s[k+1:]
	}
	a, err := parsePortNumber(lo)
	if err != nil {
		return 0, 0, err
	}
	b, err := parsePortNumber(hi)
	if err != nil {
		return 0, 0, err
	}
	if b < a {
		return 0, 0, fmt.Errorf("range %q is reversed", s)
	}
	return a, b, nil
}

func parsePortNumber(s string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 1 || n > 65535 {
		return 0, fmt.Errorf("%q is not a port number (1-65535)", s)
	}
	return n, nil
}

// parseLongPort parses {target, published, protocol, host_ip}.
func parseLongPort(m map[string]interface{}) ([]PortMapping, error) {
	str := func(k string) string {
		switch v := m[k].(type) {
		case string:
			s, _ := interpolate(v)
			return s
		case int:
			return strconv.Itoa(v)
		}
		return ""
	}
	target := str("target")
	if target == "" {
		return nil, fmt.Errorf("port %v: target is required", m)
	}
	s := target
	if pub := str("published"); pub != "" {
		s = pub + ":" + target
	}
	if ip := str("host_ip"); ip != "" {
		if !strings.Contains(s, ":") {
			s = ":" + s
		}
		if strings.Contains(ip, ":") {
			ip = "[" + ip + "]"
		}
		s = ip + ":" + s
	}
	if proto := str("protocol"); proto != "" {
		s += "/" + proto
	}
	return parsePortString(s)
}

// parseResources extracts CPU and memory limits from deploy.resources.limits.
func parseResources(svc map[string]interface{}) (Resources, error) {
	deploy, ok := svc["deploy"].(map[string]interface{})
	if !ok {
		return Resources{}, nil
	}
	resources, ok := deploy["resources"].(map[string]interface{})
	if !ok {
		return Resources{}, nil
	}
	limits, ok := resources["limits"].(map[string]interface{})
	if !ok {
		return Resources{}, nil
	}

	var res Resources
	if cpus, ok := limits["cpus"]; ok {
		f, err := parseFloat(cpus)
		// 4096 vCPU is beyond any flavor and keeps int conversions sane.
		if err != nil || f < 0 || f > 4096 {
			return Resources{}, fmt.Errorf("cpus %v is not a number of CPUs", cpus)
		}
		res.CPUCores = f
	}
	if mem, ok := limits["memory"]; ok {
		mb, err := parseMemory(mem)
		if err != nil {
			return Resources{}, fmt.Errorf("memory %v: %v", mem, err)
		}
		res.MemoryMB = mb
	}
	return res, nil
}

// parseFloat converts a YAML value (string or number) to a finite float64.
func parseFloat(v interface{}) (float64, error) {
	var f float64
	switch val := v.(type) {
	case string:
		var err error
		if f, err = strconv.ParseFloat(strings.TrimSpace(val), 64); err != nil {
			return 0, err
		}
	case float64:
		f = val
	case int:
		f = float64(val)
	default:
		return 0, fmt.Errorf("not a number")
	}
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, fmt.Errorf("not a finite number")
	}
	return f, nil
}

// maxMemoryMB bounds memory limits (64 TiB) so that sizing arithmetic
// cannot overflow.
const maxMemoryMB = 64 << 20

// parseMemory converts a Docker byte value to megabytes (MiB, rounded up):
// a bare number is bytes; units b, k, m, g with an optional trailing "b",
// case-insensitive ("512m", "1gb", "1.5G").
func parseMemory(v interface{}) (int, error) {
	var n float64
	switch val := v.(type) {
	case int:
		n = float64(val)
	case float64:
		n = val
	case string:
		s := strings.ToLower(strings.TrimSpace(val))
		mult := 1.0
		num := s
		for _, u := range []struct {
			suffix string
			mult   float64
		}{{"kb", 1 << 10}, {"mb", 1 << 20}, {"gb", 1 << 30}, {"k", 1 << 10}, {"m", 1 << 20}, {"g", 1 << 30}, {"b", 1}} {
			if strings.HasSuffix(s, u.suffix) {
				num, mult = strings.TrimSpace(strings.TrimSuffix(s, u.suffix)), u.mult
				break
			}
		}
		f, err := strconv.ParseFloat(num, 64)
		if err != nil {
			return 0, fmt.Errorf("not a byte value (use e.g. 512M or 1G)")
		}
		n = f * mult
	default:
		return 0, fmt.Errorf("not a byte value (use e.g. 512M or 1G)")
	}
	if math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || n/(1<<20) > maxMemoryMB {
		return 0, fmt.Errorf("out of range")
	}
	return int(math.Ceil(n / (1 << 20))), nil
}

// notUploaded ends the warnings about what the VM does not get.
func notUploaded() string {
	return i18n.T("deploy uploads only the compose file", "deploy загружает только compose-файл")
}

// localOnly warns about what a service takes from this machine and the VM
// will not have (guide §5): a build context, env files, bind mounts of
// relative or home paths. An absolute bind source names a path on the VM
// (/var/run/docker.sock) and is left alone.
func localOnly(svc map[string]interface{}) []string {
	var warnings []string
	if _, ok := svc["build"]; ok {
		warnings = append(warnings, i18n.Tf(
			"build: is not run on the VM: there is no build context (%s); push the image to a registry and set image:",
			"build: на ВМ не выполняется: контекста сборки там нет (%s); опубликуйте образ в реестре и укажите его в image:",
			notUploaded()))
	}
	var envFiles []string
	switch v := svc["env_file"].(type) {
	case string:
		envFiles = append(envFiles, v)
	case []interface{}:
		for _, e := range v {
			switch e := e.(type) {
			case string:
				envFiles = append(envFiles, e)
			case map[string]interface{}: // {path: ..., required: false}
				if p, ok := e["path"].(string); ok {
					envFiles = append(envFiles, p)
				}
			}
		}
	}
	if len(envFiles) > 0 {
		warnings = append(warnings, i18n.Tf(
			"env_file: %s is not on the VM (%s)",
			"env_file: %s на ВМ нет (%s)",
			strings.Join(envFiles, ", "), notUploaded()))
	}
	vols, _ := svc["volumes"].([]interface{})
	for _, v := range vols {
		var src, spec string
		switch v := v.(type) {
		case string: // SRC:DST[:MODE]; a lone DST is an anonymous volume
			if k := strings.Index(v, ":"); k > 0 {
				src, spec = v[:k], v
			}
		case map[string]interface{}:
			if t, _ := v["type"].(string); t == "bind" {
				src, _ = v["source"].(string)
				spec = src
			}
		}
		if localPath(src) {
			warnings = append(warnings, i18n.Tf(
				"volumes: %s mounts a local path, its files are not on the VM (%s)",
				"volumes: %s монтирует локальный путь, его файлов на ВМ нет (%s)",
				spec, notUploaded()))
		}
	}
	return warnings
}

// localPath reports whether a bind source is relative to the compose file
// or to the home directory, as opposed to a named volume ("data").
func localPath(src string) bool {
	return src == "." || src == ".." || src == "~" ||
		strings.HasPrefix(src, "./") || strings.HasPrefix(src, "../") || strings.HasPrefix(src, "~/")
}

// fileSources warns about the top-level secrets and configs read from a
// file: the file stays on this machine.
func fileSources(root map[string]interface{}) []string {
	var warnings []string
	for _, kind := range []string{"configs", "secrets"} {
		m, _ := root[kind].(map[string]interface{})
		names := make([]string, 0, len(m))
		for name := range m {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			def, _ := m[name].(map[string]interface{})
			if file, ok := def["file"].(string); ok && file != "" {
				warnings = append(warnings, i18n.Tf(
					"%s.%s: file %s is not on the VM (%s)",
					"%s.%s: файла %s на ВМ нет (%s)",
					kind, name, file, notUploaded()))
			}
		}
	}
	return warnings
}

// parseDependsOn extracts the depends_on list from a service definition.
func parseDependsOn(svc map[string]interface{}) []string {
	dep, ok := svc["depends_on"]
	if !ok {
		return nil
	}

	switch v := dep.(type) {
	case []interface{}:
		result := make([]string, 0, len(v))
		for _, d := range v {
			if s, ok := d.(string); ok {
				result = append(result, s)
			}
		}
		return result
	case map[string]interface{}:
		result := make([]string, 0, len(v))
		for name := range v {
			result = append(result, name)
		}
		sort.Strings(result)
		return result
	default:
		return nil
	}
}
