// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package errors

// Error code constants defined in ADR-007.
const (
	CodeAuthRequired      = "AUTH_REQUIRED"
	CodeAuthExpired       = "AUTH_EXPIRED"
	CodeAuthFailed        = "AUTH_FAILED"
	CodeAPIUnreachable    = "API_UNREACHABLE"
	CodeAPIRateLimited    = "API_RATE_LIMITED"
	CodeAPIError          = "API_ERROR"
	CodeVMNotFound        = "VM_NOT_FOUND"
	CodeVMProvisionFailed = "VM_PROVISION_FAILED"
	CodeSSHTimeout        = "SSH_TIMEOUT"
	CodeSSHAuthFailed     = "SSH_AUTH_FAILED"
	CodeSSHHostKeyChanged = "SSH_HOST_KEY_CHANGED"
	CodeComposeInvalid    = "COMPOSE_INVALID"
	CodeComposeNotFound   = "COMPOSE_NOT_FOUND"
	CodeConfigInvalid     = "CONFIG_INVALID"
	CodeStateCorrupted    = "STATE_CORRUPTED"
	CodeStateLocked       = "STATE_LOCKED" // another deploy/destroy holds the state lock; exit 1
	CodeUnhealthy         = "UNHEALTHY"    // verify: a service did not answer; exit 1
	// CodeServerBusy: evo mcp --http refused a tool call over its own limit
	// of concurrent calls; Cloud.ru was not asked (release audit, L-03).
	CodeServerBusy = "SERVER_BUSY" // exit 1

	CodeConfirmationRequired = "CONFIRMATION_REQUIRED"
	CodeUsage                = "USAGE"     // unknown command or flag, bad arguments
	CodeNotFound             = "NOT_FOUND" // any object other than a VM; exit 4
)

// Reasons of JSON errors that carry no CLIError (see Reason).
const (
	ReasonInterrupted = "INTERRUPTED" // Ctrl-C, SIGTERM; exit 130
	ReasonGeneral     = "ERROR"       // an error without a symbolic code; exit 1
)

// knownCodes is every code above: the symbolic codes a CLIError carries.
var knownCodes = map[string]bool{
	CodeAuthRequired: true, CodeAuthExpired: true, CodeAuthFailed: true,
	CodeAPIUnreachable: true, CodeAPIRateLimited: true, CodeAPIError: true,
	CodeVMNotFound: true, CodeVMProvisionFailed: true,
	CodeSSHTimeout: true, CodeSSHAuthFailed: true, CodeSSHHostKeyChanged: true,
	CodeComposeInvalid: true, CodeComposeNotFound: true,
	CodeConfigInvalid: true, CodeStateCorrupted: true, CodeStateLocked: true,
	CodeUnhealthy: true, CodeConfirmationRequired: true, CodeUsage: true,
	CodeNotFound: true, CodeServerBusy: true,
}

// IsCode reports whether s is one of the codes above ("API_UNREACHABLE").
// A tool answer carries its error as text ("API_UNREACHABLE: …"); this tells
// a code prefix from any other upper-case word.
func IsCode(s string) bool { return knownCodes[s] }
