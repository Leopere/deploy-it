package contract

import (
	"strconv"
	"testing"
	"time"
)

func TestDeploymentBudgetCapsAndReservesHardDeadline(t *testing.T) {
	now := time.Date(2026, time.September, 6, 12, 0, 0, 0, time.UTC)
	if maximumDeploymentDuration != 1800*time.Second {
		t.Fatalf("maximum deployment duration=%s want=30m", maximumDeploymentDuration)
	}
	budget, err := newDeploymentBudget(now, maximumDeploymentDuration, deadlineReserve, strconv.FormatInt(now.Add(45*time.Minute).UnixMilli(), 10))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := budget.hard, now.Add(30*time.Minute); !got.Equal(want) {
		t.Fatalf("hard=%s want=%s", got, want)
	}
	if got, want := budget.executes, now.Add(30*time.Minute-deadlineReserve); !got.Equal(want) {
		t.Fatalf("execution deadline=%s want=%s", got, want)
	}

	configured := now.Add(20 * time.Minute)
	budget, err = newDeploymentBudget(now, maximumDeploymentDuration, deadlineReserve, strconv.FormatInt(configured.UnixMilli(), 10))
	if err != nil {
		t.Fatal(err)
	}
	if !budget.hard.Equal(configured) || !budget.executes.Equal(configured.Add(-deadlineReserve)) {
		t.Fatalf("budget=%#v", budget)
	}
}

func TestCurrentDeploymentBudgetUsesThirtyMinuteMaximum(t *testing.T) {
	t.Setenv("SHIP_IT_DEADLINE_UNIX_MS", "")
	before := time.Now()
	budget, err := currentDeploymentBudget()
	after := time.Now()
	if err != nil {
		t.Fatal(err)
	}
	if budget.hard.Before(before.Add(30*time.Minute)) || budget.hard.After(after.Add(30*time.Minute)) {
		t.Fatalf("hard deadline=%s is not derived from the 30-minute maximum", budget.hard)
	}
	if got, want := budget.executes, budget.hard.Add(-deadlineReserve); !got.Equal(want) {
		t.Fatalf("execution deadline=%s want=%s", got, want)
	}
}
