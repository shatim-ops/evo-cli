// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

// Package clock is the clock of the values evo derives from the time of a
// call and puts into an answer: the default name of a VM or a plan, the
// name of a boot floating IP. Two calls that should answer alike (a
// command and its tool, a server with --read-only and one without) differ
// there by the time alone, and the parity tests compare them with these
// values masked. A clock that tests move makes a value that slips past the
// mask differ every time, not once a minute boundary falls between two
// calls (the unstable TestReadOnly_EveryTool of 1.2.2, process audit
// 1.2.1, PR 4). Waits, deadlines and timestamps of logs keep time.Now.
package clock

import "time"

// Now is the time of a call for the values above.
var Now = time.Now
