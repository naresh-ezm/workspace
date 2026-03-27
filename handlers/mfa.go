package handlers

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image/png"

	"github.com/gofiber/fiber/v2"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"

	"ec2manager/middleware"
	"ec2manager/models"
)

type mfaSetupData struct {
	BaseData
	QRCodePNG string // base64-encoded PNG — embedded directly as <img src>
	Secret    string
	Error     string
}

// MFASetupPage generates a TOTP secret, saves it (unenabled), and renders the
// QR code setup page (GET /mfa/setup).
func (h *Handler) MFASetupPage(c *fiber.Ctx) error {
	user := middleware.GetUser(c)

	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "EC2Manager",
		AccountName: user.Username,
		Algorithm:   otp.AlgorithmSHA1,
	})
	if err != nil {
		h.Logger.Error("TOTP key generation failed", "error", err)
		return fiber.ErrInternalServerError
	}

	if err := models.SaveTOTPSecret(h.DB, user.ID, key.Secret()); err != nil {
		h.Logger.Error("failed to save TOTP secret", "error", err)
		return fiber.ErrInternalServerError
	}

	qrPNG, err := keyToBase64PNG(key)
	if err != nil {
		h.Logger.Error("failed to generate QR code", "error", err)
		return fiber.ErrInternalServerError
	}

	return h.render(c, "mfa_setup", mfaSetupData{
		BaseData:  BaseData{CurrentUser: user},
		QRCodePNG: qrPNG,
		Secret:    key.Secret(),
	})
}

// MFASetup verifies the first TOTP code and activates MFA for the user
// (POST /mfa/setup).
func (h *Handler) MFASetup(c *fiber.Ctx) error {
	user := middleware.GetUser(c)

	freshUser, err := models.GetUserByID(h.DB, user.ID)
	if err != nil || !freshUser.TOTPSecret.Valid {
		return h.render(c, "mfa_setup", mfaSetupData{
			BaseData: BaseData{CurrentUser: user},
			Error:    "Setup session expired. Please start again.",
		})
	}

	if !totp.Validate(c.FormValue("code"), freshUser.TOTPSecret.String) {
		// Re-generate the QR image from the stored secret so the page still renders.
		key, err := otp.NewKeyFromURL(fmt.Sprintf(
			"otpauth://totp/EC2Manager:%s?secret=%s&issuer=EC2Manager",
			user.Username, freshUser.TOTPSecret.String,
		))
		qrPNG := ""
		if err == nil {
			qrPNG, _ = keyToBase64PNG(key)
		}
		return h.render(c, "mfa_setup", mfaSetupData{
			BaseData:  BaseData{CurrentUser: user},
			QRCodePNG: qrPNG,
			Secret:    freshUser.TOTPSecret.String,
			Error:     "Incorrect code. Make sure your device time is correct and try again.",
		})
	}

	if err := models.EnableTOTP(h.DB, user.ID); err != nil {
		h.Logger.Error("failed to enable TOTP", "user_id", user.ID, "error", err)
		return fiber.ErrInternalServerError
	}

	// Invalidate existing sessions so any re-login goes through MFA from the start.
	_ = models.DeleteUserSessions(h.DB, user.ID)

	h.Logger.Info("MFA enabled", "user", user.Username)
	return c.Redirect("/dashboard?success=MFA+enabled+successfully", fiber.StatusSeeOther)
}

// keyToBase64PNG renders a TOTP key's QR code as a base64-encoded PNG string
// suitable for embedding directly in an <img src="data:image/png;base64,..."> tag.
func keyToBase64PNG(key *otp.Key) (string, error) {
	img, err := key.Image(256, 256)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

// MFADisable verifies the current TOTP code and turns off MFA for the
// authenticated user (POST /mfa/disable).
func (h *Handler) MFADisable(c *fiber.Ctx) error {
	user := middleware.GetUser(c)

	freshUser, err := models.GetUserByID(h.DB, user.ID)
	if err != nil {
		return fiber.ErrInternalServerError
	}
	if !freshUser.TOTPEnabled {
		return c.Redirect("/dashboard", fiber.StatusSeeOther)
	}

	if !totp.Validate(c.FormValue("code"), freshUser.TOTPSecret.String) {
		h.Logger.Warn("invalid TOTP code on disable attempt", "user", user.Username)
		return c.Redirect("/dashboard?error=Incorrect+MFA+code.+MFA+not+disabled.", fiber.StatusSeeOther)
	}

	if err := models.DisableTOTP(h.DB, user.ID); err != nil {
		h.Logger.Error("failed to disable TOTP", "user_id", user.ID, "error", err)
		return fiber.ErrInternalServerError
	}

	h.Logger.Info("MFA disabled by user", "user", user.Username)
	return c.Redirect("/dashboard?success=MFA+disabled+successfully", fiber.StatusSeeOther)
}
