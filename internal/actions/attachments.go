package actions

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/GabiHert/hadron/internal/attachments"
)

// AttachmentSaveIn is the input of attachment_save.
type AttachmentSaveIn struct {
	Name string `json:"name,omitempty" jsonschema:"the file's original name, used in messages only"`
	Data string `json:"data" jsonschema:"the file's bytes in base64: a PNG, JPEG, GIF or WebP image, or a PDF, at most 20 MB"`
}

func registerAttachments(r *Registry) {
	Register(r, Spec{
		Name:        "attachment_save",
		Description: "Keep an image or PDF the user pasted, dropped or picked, under <home>/attachments/<date>/<uuid>.<ext>, and return its absolute path (which Claude Code reads) and, for an image, the url the window shows its thumbnail from. Only the user attaches files; at most 20 MB.",
		Sensitive:   true,
	},
		func(ctx context.Context, c *Call, in AttachmentSaveIn) (attachments.Saved, error) {
			if c.Actor.Kind != KindYou {
				return attachments.Saved{}, errors.New("only the user attaches files: a session reads a file by its path instead")
			}
			if c.Deps.Home == "" {
				return attachments.Saved{}, errors.New("attachments need Hadron's data folder, which this build does not have")
			}
			if base64.StdEncoding.DecodedLen(len(in.Data)) > attachments.MaxBytes+3 {
				return attachments.Saved{}, attachments.ErrTooLarge
			}
			data, err := base64.StdEncoding.DecodeString(in.Data)
			if err != nil {
				return attachments.Saved{}, fmt.Errorf("data is not base64: %w", err)
			}
			return attachments.Save(c.Deps.Home, in.Name, data, time.Now())
		})
}
