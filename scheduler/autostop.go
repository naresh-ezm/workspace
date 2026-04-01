package scheduler

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	awsclient "ec2manager/aws"
	"ec2manager/models"
)

const (
	// checkInterval is how often the scheduler wakes up.
	checkInterval = 10 * time.Minute

	// idleThresholdWeekday is the max idle time before stopping on Mon-Fri.
	idleThresholdWeekday = 2 * time.Hour

	// idleThresholdWeekend is the max idle time before stopping on Sat-Sun.
	idleThresholdWeekend = 30 * time.Minute

	// heartbeatGrace is how long without a heartbeat before we treat an
	// instance as idle regardless of its last recorded status.
	heartbeatGrace = 20 * time.Minute
)

// Scheduler runs the auto-stop background worker.
type Scheduler struct {
	db     *sql.DB
	ec2    *awsclient.EC2Client
	logger *slog.Logger
	ctx    context.Context
	cancel context.CancelFunc
}

// New creates a Scheduler but does not start it.
func New(db *sql.DB, ec2 *awsclient.EC2Client, logger *slog.Logger) *Scheduler {
	ctx, cancel := context.WithCancel(context.Background())
	return &Scheduler{
		db:     db,
		ec2:    ec2,
		logger: logger,
		ctx:    ctx,
		cancel: cancel,
	}
}

// Start launches the background goroutine.
func (s *Scheduler) Start() {
	s.logger.Info("auto-stop scheduler started", "interval", checkInterval)
	go s.loop()
}

// Stop signals the scheduler goroutine to exit.
func (s *Scheduler) Stop() {
	s.cancel()
}

func (s *Scheduler) loop() {
	// Run once immediately on startup, then on every tick.
	s.runCheck()

	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.runCheck()
		case <-s.ctx.Done():
			s.logger.Info("auto-stop scheduler stopped")
			return
		}
	}
}

func (s *Scheduler) runCheck() {
	s.logger.Debug("auto-stop check running")

	instances, err := models.ListInstances(s.db)
	if err != nil {
		s.logger.Error("scheduler: failed to list instances", "error", err)
		return
	}

	for _, inst := range instances {
		s.evaluateInstance(inst)
	}
}

func (s *Scheduler) evaluateInstance(inst *models.Instance) {
	ctx, cancel := context.WithTimeout(s.ctx, 30*time.Second)
	defer cancel()

	// Only stop instances that are actually running.
	running, err := s.ec2.IsRunning(ctx, inst.InstanceID)
	if err != nil {
		s.logger.Warn("scheduler: cannot determine instance state",
			"instance_id", inst.InstanceID, "error", err)
		return
	}
	if !running {
		return
	}

	// Determine effective last-active time.
	lastActive := s.effectiveLastActive(inst)
	idleDuration := time.Since(lastActive)
	threshold := idleThreshold()

	s.logger.Debug("scheduler: instance idle check",
		"instance_id", inst.InstanceID,
		"idle_duration", idleDuration.Round(time.Second),
		"threshold", threshold,
		"status", inst.Status,
	)

	if idleDuration <= threshold {
		return // still within allowed idle window
	}

	// Stop the instance.
	s.logger.Info("scheduler: stopping idle instance",
		"instance_id", inst.InstanceID,
		"idle_duration", idleDuration.Round(time.Second),
		"threshold", threshold,
	)

	stopCtx, stopCancel := context.WithTimeout(s.ctx, 30*time.Second)
	defer stopCancel()

	if err := s.ec2.StopInstance(stopCtx, inst.InstanceID); err != nil {
		s.logger.Error("scheduler: StopInstance failed",
			"instance_id", inst.InstanceID, "error", err)
		return
	}

	// Reset the idle timer so that if a developer restarts the instance the
	// scheduler does not immediately re-stop it due to the stale timestamps.
	if err := models.ResetInstanceTimers(s.db, inst.InstanceID); err != nil {
		s.logger.Error("scheduler: failed to reset instance timers", "error", err)
	}

	meta := fmt.Sprintf(
		`{"idle_seconds":%d,"threshold_seconds":%d,"weekday":"%s"}`,
		int(idleDuration.Seconds()),
		int(threshold.Seconds()),
		time.Now().Weekday(),
	)
	if err := models.CreateLog(s.db, nil, models.ActionAutoStop, inst.InstanceID, meta); err != nil {
		s.logger.Error("scheduler: failed to write auto-stop log", "error", err)
	}
}

// effectiveLastActive returns the time we should treat as "last user activity".
// Rules:
//  1. If no heartbeat has ever been received, assume very idle.
//  2. If the last heartbeat is stale (> heartbeatGrace), treat the heartbeat
//     time itself as the last-active time (instance went dark).
//  3. Otherwise use last_active_at (set when status == "active").
func (s *Scheduler) effectiveLastActive(inst *models.Instance) time.Time {
	if !inst.LastHeartbeatAt.Valid {
		// No heartbeat data at all – the instance has been running without our
		// monitor script.  Be conservative: treat as idle from a very long time.
		return time.Now().Add(-24 * time.Hour)
	}

	lastHeartbeat := inst.LastHeartbeatAt.Time

	// Stale heartbeat – the monitor script may have stopped.
	if time.Since(lastHeartbeat) > heartbeatGrace {
		// Use the heartbeat time as the last known point of activity.
		return lastHeartbeat
	}

	// Fresh heartbeat – use last_active_at when available.
	if inst.LastActiveAt.Valid {
		return inst.LastActiveAt.Time
	}

	// Heartbeat is fresh but only idle pings have been received; use the
	// heartbeat time as a proxy for when the idleness began.
	return lastHeartbeat
}

// idleThreshold returns the allowed idle duration based on the current day.
func idleThreshold() time.Duration {
	switch time.Now().Weekday() {
	case time.Saturday, time.Sunday:
		return idleThresholdWeekend
	default:
		return idleThresholdWeekday
	}
}
