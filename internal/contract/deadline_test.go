package contract

import (
	"strconv"
	"testing"
	"time"
)

func TestDeploymentBudgetCapsAndReservesHardDeadline(t *testing.T) {
	now := time.Date(2026, time.September, 6, 12, 0, 0, 0, time.UTC)
	budget, err := newDeploymentBudget(now, 300*time.Second, 5*time.Second, strconv.FormatInt(now.Add(10*time.Minute).UnixMilli(), 10))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := budget.hard, now.Add(300*time.Second); !got.Equal(want) {
		t.Fatalf("hard=%s want=%s", got, want)
	}
	if got, want := budget.executes, now.Add(295*time.Second); !got.Equal(want) {
		t.Fatalf("execution deadline=%s want=%s", got, want)
	}

	configured := now.Add(20 * time.Second)
	budget, err = newDeploymentBudget(now, 300*time.Second, 5*time.Second, strconv.FormatInt(configured.UnixMilli(), 10))
	if err != nil {
		t.Fatal(err)
	}
	if !budget.hard.Equal(configured) || !budget.executes.Equal(configured.Add(-5*time.Second)) {
		t.Fatalf("budget=%#v", budget)
	}
}
