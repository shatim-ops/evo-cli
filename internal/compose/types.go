// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package compose

import "strings"

// PortMapping represents a host:container port pair.
type PortMapping struct {
	Host      int // 0: Docker picks a random host port
	Container int
	Protocol  string // "tcp" or "udp", default "tcp"
	HostIP    string // "" or the address the host port is bound to
}

// Public reports whether the mapping is reachable from outside the VM on a
// known port.
func (p PortMapping) Public() bool {
	if p.Host == 0 {
		return false
	}
	switch p.HostIP {
	case "::1", "localhost":
		return false
	}
	return !strings.HasPrefix(p.HostIP, "127.")
}

// Resources represents aggregated CPU/memory needs.
type Resources struct {
	CPUCores float64
	MemoryMB int
}

// Service represents a single service from docker-compose.yml.
type Service struct {
	Name      string
	Image     string
	Ports     []PortMapping
	Resources Resources
	DependsOn []string
}

// Project represents a parsed docker-compose.yml.
type Project struct {
	Services  []Service
	Ports     []int     // public TCP host ports, deduplicated and sorted
	UDPPorts  []int     // public UDP host ports, deduplicated and sorted
	Resources Resources // aggregated across all services
	Hash      string    // SHA-256 of raw file content
	Warnings  []string  // ports whose host side is not known before 'up'
}
