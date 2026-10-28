// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"strings"
	"testing"
)

// Release plan 1.5, С9: skills/evo-scenarios tells the agent that evo has
// no tools for the topics and users of a Kafka cluster — the person makes
// them in the console. A tool that does it takes the line off the skill
// together with this test.
func TestKafka_NoTopicOrUserTools(t *testing.T) {
	for _, tl := range New(nil).Tools() {
		if strings.HasPrefix(tl.Name, "kafka_") && (strings.Contains(tl.Name, "topic") || strings.Contains(tl.Name, "user")) {
			t.Errorf("%s: a Kafka topic or user tool — change skills/evo-scenarios, which sends the person to the console", tl.Name)
		}
	}
}
