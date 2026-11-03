package service

import (
	"context"
	"time-leak/internal/domain"
)

// IAdminReminderService exposes aggregate settings/status without recipient data.
type IAdminReminderService interface {
	GetReminderSettings(context.Context) (domain.ReminderSettings, error)
	UpdateReminderSettings(context.Context, domain.ReminderSettings) (domain.ReminderSettings, error)
	ReminderStatus(context.Context) (domain.ReminderStatus, error)
}
