package handlers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	awsclient "ec2manager/aws"
	"ec2manager/middleware"
	"ec2manager/models"
)

type dashboardData struct {
	BaseData
	AWSInstance *awsclient.InstanceInfo
	DBInstance  *models.Instance
	Error       string
	Success     string
}

// Dashboard renders the developer's instance control panel (GET /dashboard).
func (h *Handler) Dashboard(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	data := dashboardData{
		BaseData: BaseData{CurrentUser: user},
		Success:  r.URL.Query().Get("success"),
		Error:    r.URL.Query().Get("error"),
	}

	if !user.InstanceID.Valid || user.InstanceID.String == "" {
		data.Error = "No EC2 instance has been assigned to your account. Contact an administrator."
		h.render(w, h.Tmpls.Dashboard, data)
		return
	}

	instID := user.InstanceID.String

	// Fetch live AWS state.
	awsInfo, err := h.EC2.DescribeInstance(r.Context(), instID)
	if err != nil {
		h.Logger.Error("describe instance failed", "instance_id", instID, "error", err)
		if data.Error == "" {
			data.Error = fmt.Sprintf("Could not retrieve instance status: %v", err)
		}
	} else {
		data.AWSInstance = awsInfo
	}

	// Fetch heartbeat metadata from DB (best-effort).
	dbInst, err := models.GetInstance(h.DB, instID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		h.Logger.Warn("failed to read instance heartbeat record", "instance_id", instID, "error", err)
	}
	data.DBInstance = dbInst

	h.render(w, h.Tmpls.Dashboard, data)
}

// StartInstance calls EC2 StartInstances (POST /start-instance).
func (h *Handler) StartInstance(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)

	instID, ok := h.requireInstance(w, user)
	if !ok {
		return
	}

	if err := h.EC2.StartInstance(context.Background(), instID); err != nil {
		h.Logger.Error("StartInstance failed", "instance_id", instID, "error", err)
		h.redirectDashboard(w, r, "", fmt.Sprintf("Failed to start instance: %v", err))
		return
	}

	h.logAction(&user.ID, models.ActionStart, instID, `{"triggered_by":"user"}`)
	h.Logger.Info("instance started", "instance_id", instID, "user", user.Username)
	h.redirectDashboard(w, r, "Instance is starting… it may take a minute to become fully available.", "")
}

// StopInstance calls EC2 StopInstances (POST /stop-instance).
func (h *Handler) StopInstance(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)

	instID, ok := h.requireInstance(w, user)
	if !ok {
		return
	}

	if err := h.EC2.StopInstance(context.Background(), instID); err != nil {
		h.Logger.Error("StopInstance failed", "instance_id", instID, "error", err)
		h.redirectDashboard(w, r, "", fmt.Sprintf("Failed to stop instance: %v", err))
		return
	}

	h.logAction(&user.ID, models.ActionStop, instID, `{"triggered_by":"user"}`)
	h.Logger.Info("instance stopped", "instance_id", instID, "user", user.Username)
	h.redirectDashboard(w, r, "Instance is stopping…", "")
}

// ──────────────────────────────────────────────────────────────
// Internal helpers
// ──────────────────────────────────────────────────────────────

func (h *Handler) requireInstance(w http.ResponseWriter, user *models.User) (string, bool) {
	if !user.InstanceID.Valid || user.InstanceID.String == "" {
		http.Error(w, "No instance assigned to your account", http.StatusBadRequest)
		return "", false
	}
	return user.InstanceID.String, true
}

// redirectDashboard sends the user back to /dashboard with optional flash
// messages encoded as query parameters.
func (h *Handler) redirectDashboard(w http.ResponseWriter, r *http.Request, success, errMsg string) {
	q := url.Values{}
	if success != "" {
		q.Set("success", success)
	}
	if errMsg != "" {
		q.Set("error", errMsg)
	}
	target := "/dashboard"
	if len(q) > 0 {
		target += "?" + q.Encode()
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}
