// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package secretsent

import (
	"context"
	"sync"
	"testing"
)

// A step marks the call it is made in (a stack's answer carries the step's
// error); a mark goes up, never down or across: the call's other steps and
// other calls stay unmarked.
func TestSecretSent_AStepMarksTheCallItIsMadeIn(t *testing.T) {
	call := Watch(context.Background())
	step, other := Watch(call), Watch(call)
	Mark(step)
	if !Sent(call) || !Sent(step) {
		t.Errorf("a step marked: the call %v, the step %v; want both", Sent(call), Sent(step))
	}
	if Sent(other) {
		t.Error("a step of the same call no value was sent in is marked")
	}
	if Sent(Watch(context.Background())) {
		t.Error("a call no step of marked is marked")
	}
	if Sent(Watch(step)) {
		t.Error("a new step of a marked step is marked before it sends anything")
	}
}

// A ctx no call watches (a client used outside a tool) is not marked and
// does not fail.
func TestSecretSent_UnwatchedIsNotMarked(t *testing.T) {
	ctx := context.Background()
	Mark(ctx)
	if Sent(ctx) {
		t.Error("an unwatched ctx is marked")
	}
}

// Steps of a stack may run at once: their marks race to the call's (go test
// -race).
func TestSecretSent_StepsMarkAtOnce(t *testing.T) {
	call := Watch(context.Background())
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			Mark(Watch(call))
			_ = Sent(call)
		}()
	}
	wg.Wait()
	if !Sent(call) {
		t.Error("the call is not marked after its steps")
	}
}
