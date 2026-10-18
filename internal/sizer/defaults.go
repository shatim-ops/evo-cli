// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package sizer

import (
	"strings"

	"github.com/shatim-ops/evo-cli/internal/compose"
)

// ImageDefaults maps well-known container images to estimated resource needs.
var ImageDefaults = map[string]compose.Resources{
	"postgres":  {CPUCores: 1.0, MemoryMB: 512},
	"mysql":     {CPUCores: 1.0, MemoryMB: 512},
	"redis":     {CPUCores: 0.5, MemoryMB: 256},
	"mongo":     {CPUCores: 1.0, MemoryMB: 512},
	"nginx":     {CPUCores: 0.25, MemoryMB: 128},
	"httpd":     {CPUCores: 0.25, MemoryMB: 128},
	"rabbitmq":  {CPUCores: 0.5, MemoryMB: 256},
	"memcached": {CPUCores: 0.25, MemoryMB: 128},
}

// fallback resources for images not found in ImageDefaults.
var fallbackResources = compose.Resources{CPUCores: 0.5, MemoryMB: 256}

// EstimateFromServices calculates total resources by looking up image defaults
// for services that don't have explicit resource limits.
func EstimateFromServices(services []compose.Service) compose.Resources {
	var total compose.Resources
	for _, svc := range services {
		r := resourcesForService(svc)
		total.CPUCores += r.CPUCores
		total.MemoryMB += r.MemoryMB
	}
	return total
}

// resourcesForService returns explicit resources if set, otherwise looks up
// the image in ImageDefaults, falling back to a conservative default.
func resourcesForService(svc compose.Service) compose.Resources {
	def := fallbackResources
	if d, ok := ImageDefaults[baseImage(svc.Image)]; ok {
		def = d
	}
	// A limit on one dimension keeps the default for the other: cpus alone
	// must not size the service at 0 MB.
	r := svc.Resources
	if r.CPUCores <= 0 {
		r.CPUCores = def.CPUCores
	}
	if r.MemoryMB <= 0 {
		r.MemoryMB = def.MemoryMB
	}
	return r
}

// baseImage strips the registry, path, tag and digest from an image
// reference, returning just the short image name. For example:
//
//	"postgres:15"                -> "postgres"
//	"library/redis:7-alpine"     -> "redis"
//	"localhost:5000/postgres"    -> "postgres" (a registry port is not a tag)
//	"postgres@sha256:..."        -> "postgres"
func baseImage(image string) string {
	if idx := strings.Index(image, "@"); idx != -1 {
		image = image[:idx]
	}
	// Take the last path component (handles registry/org prefixes).
	if idx := strings.LastIndex(image, "/"); idx != -1 {
		image = image[idx+1:]
	}
	if idx := strings.LastIndex(image, ":"); idx != -1 {
		image = image[:idx]
	}
	return image
}
