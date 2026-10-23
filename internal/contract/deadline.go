package contract

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

const (
	maximumDeploymentDuration = 30 * time.Minute
	deadlineReserve           = 5 * time.Second
)

var errDeploymentDeadline = errors.New("deploy-it total deadline exceeded")

// deploymentBudget is fixed when a Runner is opened. Every Git operation and
// deployment command derives a context from the same deadline; no stage gets a
// new timeout.
type deploymentBudget struct {
	hard     time.Time
	executes time.Time
	hardMS   string
}

func newDeploymentBudget(now time.Time, maximum, reserve time.Duration, configured string) (deploymentBudget, error) {
	if maximum <= 0 || reserve < 0 || reserve >= maximum {
		return deploymentBudget{}, errors.New("invalid deployment deadline configuration")
	}
	hard := now.Add(maximum)
	if configured != "" {
		milliseconds, err := strconv.ParseInt(configured, 10, 64)
		if err != nil || milliseconds <= 0 {
			return deploymentBudget{}, errors.New("SHIP_IT_DEADLINE_UNIX_MS must be an absolute Unix timestamp in milliseconds")
		}
		configuredDeadline := time.UnixMilli(milliseconds)
		if configuredDeadline.Before(hard) {
			hard = configuredDeadline
		}
	}
	executes := hard.Add(-reserve)
	if !executes.After(now) {
		return deploymentBudget{}, fmt.Errorf("%w: less than %s remains for command cleanup", errDeploymentDeadline, reserve)
	}
	return deploymentBudget{hard: hard, executes: executes, hardMS: strconv.FormatInt(hard.UnixMilli(), 10)}, nil
}

func currentDeploymentBudget() (deploymentBudget, error) {
	return newDeploymentBudget(time.Now(), maximumDeploymentDuration, deadlineReserve, os.Getenv("SHIP_IT_DEADLINE_UNIX_MS"))
}

func (b deploymentBudget) context() (context.Context, context.CancelFunc) {
	return context.WithDeadline(context.Background(), b.executes)
}

func deadlineError(ctx context.Context) error {
	if ctx.Err() != nil {
		return errDeploymentDeadline
	}
	return nil
}
